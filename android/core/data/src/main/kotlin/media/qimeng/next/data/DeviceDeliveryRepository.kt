package media.qimeng.next.data

import android.content.Context
import android.util.Base64
import androidx.work.*
import com.google.gson.Gson
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import media.qimeng.next.model.*
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.sdk.model.*
import media.qimeng.next.model.Session
import java.io.File
import java.security.SecureRandom
import java.util.UUID
import java.util.concurrent.TimeUnit

interface DeviceDeliveryHost { fun isSelectedLocalInstance(target: Session): Boolean }
private class WaitingForDeviceBackup : Exception()

private data class DeliveryAuth(val publisher: String, val address: String, val device: String, val label: String,
    val encrypted: String, val credentialId: String?, val pendingKey: String? = null, val previous: String? = null)
internal data class ReceivedSegment(val kind: String, val state: String, val hash: String? = null, val receipt: String? = null, val code: String? = null)
internal fun ReceivedSegment.toOutcome() = DeviceApplyOutcome(kind = DeviceSegmentKind.entries.first { it.value == kind },
    state = DeviceApplyOutcome.State.entries.first { it.value == state }, sha256 = hash, receiptId = receipt, code = code)
private data class DeliveryRun(val task: String, val publisher: String, val device: String, val destination: String,
    val results: List<ReceivedSegment> = emptyList(), val replaceAuthorProfiles: Boolean = false)

class DeviceDeliveryRepository internal constructor(private val context: Context, private val settings: SecureSettings,
    private val provider: ApiProvider, private val backups: MaintenanceBackupRepository) {
    private val prefs = context.getSharedPreferences("device-delivery", Context.MODE_PRIVATE)
    private val gson = Gson()
    private val lock = Mutex()
    private val changed = MutableStateFlow(0L)
    val revision = changed.asStateFlow()
    private val staging = File(context.filesDir, "device-delivery")
    private fun auth(): DeliveryAuth? = prefs.getString("auth", null)?.let { gson.fromJson(it, DeliveryAuth::class.java) }
    // An unfinished protocol-1 journal remains historical. Only native tasks can resume writes.
    private fun run(): DeliveryRun? = prefs.getString("nativeRun", null)?.let { gson.fromJson(it, DeliveryRun::class.java) }
    private fun save(key: String, value: Any?) {
        check(prefs.edit().putString(key, value?.let { gson.toJson(it) }).commit()) { "设备领取状态未能保存" }; changed.update { it + 1 }
    }
    private fun saveRun(value: DeliveryRun?) = save("nativeRun", value)
    private fun notice(value: String) { check(prefs.edit().putString("notice", value).commit()); changed.update { it + 1 } }

    @Synchronized private fun installation(): String {
        prefs.getString("installation", null)?.let { return it }
        val id = "android-${UUID.randomUUID()}"
        check(prefs.edit().putString("installation", id).commit()) { "无法保存本机设备身份" }
        return id
    }
    fun view(): DeviceDeliverySettings {
        val auth = auth()
        return DeviceDeliverySettings(auth?.device ?: prefs.getString("device", null) ?: installation(), auth?.label.orEmpty(),
            auth?.address ?: prefs.getString("address", null).orEmpty(), auth?.credentialId != null, prefs.getBoolean("background", false),
            prefs.getString("destination", null), prefs.getString("notice", null), prefs.getBoolean("replaceAuthorProfiles", false))
    }
    internal fun local(target: Session): Boolean = (context.applicationContext as? DeviceDeliveryHost)?.isSelectedLocalInstance(target) == true
    private fun pin(address: String): String? = settings.session.value?.takeIf { it.baseUrl == address }?.let(settings::certificateFor)
    private fun aad(address: String, publisher: String, id: String) = operationKey("device", address, publisher, id)
    private suspend fun connection(authorization: DeliveryAuth): DefaultApi {
        val pinned = pin(authorization.address)
        val instance = provider.anonymous(authorization.address, pinned).getInstance().bodyOrThrow()
        check(instance.id == authorization.publisher) { "领取地址已指向另一影库，请核对设备授权" }
        val secret = settings.openDeviceSecret(aad(authorization.address, authorization.publisher, authorization.device), authorization.encrypted)
        val api = provider.scoped(authorization.address, pinned, secret)
        val identity = api.getCurrentSession().bodyOrThrow()
        check(identity.id == authorization.credentialId && identity.deviceId == authorization.device && identity.scopes.toSet() == setOf("device.apply")) {
            "需要由领取服务端签发并绑定此设备的专用授权"
        }
        return api
    }
    suspend fun saveSettings(target: Session, address: String, id: String, secret: String, background: Boolean, replaceAuthorProfiles: Boolean) = withContext(Dispatchers.IO) { lock.withLock {
        val device = id.trim()
        require(device.isNotEmpty()) { "请填写设备标签" }
        val url = normalizedServer(address.ifBlank { target.baseUrl })
        val editor = prefs.edit().putString("device", device).putString("address", address.trim()).putBoolean("replaceAuthorProfiles", replaceAuthorProfiles)
        val authorization = if (secret.isNotBlank()) {
            validateCredential(url, device, secret.trim()).also { editor.putString("auth", gson.toJson(it)) }
        } else {
            val saved = auth()
            check(saved == null || (saved.address == url && saved.device == device)) { "更换领取服务端或设备标签时，请填写相应的专用令牌" }
            saved
        }
        check(sameTarget(target, settings.session.value)) { "配置过程中连接已改变" }
        val previousDestination = prefs.getString("destination", null)
        val destination = if (background) {
            check(authorization?.credentialId != null) { "请先配置设备领取授权" }
            if (local(target)) target.instanceId else checkNotNull(previousDestination) { "请连接本机影库后开启后台领取" }
        } else null
        check(editor.putBoolean("background", background).putString("destination", destination)
            .putString("notice", "已保存设备领取设置").commit()) { "设备领取设置未能保存" }
        // The worker reads the two choices together when it claims a package.
        updatePolling(background)
        changed.update { it + 1 }
    } }

    private suspend fun validateCredential(url: String, device: String, secret: String): DeliveryAuth {
        val pin = pin(url)
        val instance = provider.anonymous(url, pin).getInstance().bodyOrThrow()
        val candidate = provider.scoped(url, pin, secret)
        val identity = candidate.getCurrentSession().bodyOrThrow()
        check(identity.deviceId == device && identity.scopes.toSet() == setOf("device.apply")) { "密钥未绑定此设备或不是专用领取授权" }
        candidate.listDeviceApplyTasks(targetDeviceId = device, limit = 1).bodyOrThrow()
        check(run()?.let { it.publisher != instance.id || it.device != device } != true) { "先完成原设备上已领取的包" }
        val cipher = settings.sealDeviceSecret(aad(url, instance.id, device), secret)
        return DeliveryAuth(instance.id, url, device, device, cipher, identity.id)
    }
    suspend fun generate(target: Session, id: String, label: String) = withContext(Dispatchers.IO) { lock.withLock {
        require(id.isNotBlank() && label.isNotBlank()) { "请填写设备身份与名称" }
        val device = id.trim()
        check(run()?.let { it.publisher != target.instanceId || it.device != device } != true) { "先完成原设备上已领取的包" }
        val api = provider.api(target)
        val prior = auth()
        val issuing = prior?.takeIf { it.pendingKey != null && it.address == target.baseUrl && it.publisher == target.instanceId && it.device == device && it.label == label.trim() } ?: run {
            val result = api.getDeviceCredential(device)
            val previous = if (result.code() == 404) null else result.bodyOrThrow()
            val raw = ByteArray(32).also { SecureRandom().nextBytes(it) }
            val secret = Base64.encodeToString(raw, Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)
            DeliveryAuth(target.instanceId,target.baseUrl,device,label.trim(),settings.sealDeviceSecret(aad(target.baseUrl,target.instanceId,device),secret),
                null,UUID.randomUUID().toString(),previous?.id).also { save("auth", it) }
        }
        val secret = settings.openDeviceSecret(aad(issuing.address,issuing.publisher,issuing.device), issuing.encrypted)
        val saved = api.createDeviceCredential(requireNotNull(issuing.pendingKey), DeviceCredentialInput(deviceId = device,label = issuing.label,
            credentialSecret = secret, replaceCredentialId = issuing.previous)).bodyOrThrow()
        check(saved.deviceId == device) { "签发回执不属于此设备" }
        val final = issuing.copy(credentialId = saved.id, pendingKey = null, previous = null)
        connection(final)
        save("auth", final); notice("专用授权已保存在本机")
    } }
    suspend fun recent(): List<DevicePackage> = withContext(Dispatchers.IO) {
        val identity = auth()?.takeIf { it.credentialId != null } ?: return@withContext emptyList()
        val remote = connection(identity).listDeviceApplyTasks(targetDeviceId = identity.device, limit = 20).bodyOrThrow().items.map { task ->
            DevicePackage(task.id, task.state.value, task.createdAt, task.results.orEmpty().map { outcome ->
                val segment = task.segments.singleOrNull { it.kind == outcome.kind }
                val binding = segment?.takeIf { it.kind == DeviceSegmentKind.nativeSelection }?.let { native ->
                    val destination = requireNotNull(native.targetInstanceId) { "原生设备记录缺少目标实例" }
                    DeviceSegments.nativeBinding(task, identity.publisher, destination)
                }
                val legacy = segment?.takeIf { it.kind == DeviceSegmentKind.v2Backup }?.let { LegacyReceiptBinding(it.sourceId, it.sha256, null) }
                DeviceSegmentResult(outcome.kind.value, outcome.state.value, outcome.code, outcome.receiptId, binding, legacy)
            })
        }
        val old = prefs.getString("run", null)?.let { gson.fromJson(it, DeliveryRun::class.java) }
        if (old == null || old.publisher != identity.publisher || old.device != identity.device || old.destination != settings.session.value?.instanceId) return@withContext remote
        // A protocol-1 import may have completed locally before the old app sent its result. Display that receipt without reopening the job.
        val reported = remote.firstOrNull { it.id == old.task }?.results.orEmpty().mapNotNull { it.receipt }.toSet()
        val retained = old.results.filter { it.receipt != null && it.receipt !in reported }.map {
            val legacy = if (it.kind == "v2Backup") LegacyReceiptBinding(null, it.hash, old.destination) else null
            DeviceSegmentResult(it.kind, it.state, it.code, it.receipt, legacy = legacy)
        }
        if (retained.isEmpty()) remote else listOf(DevicePackage(old.task, "legacy_local", "", retained)) + remote
    }
    suspend fun receipt(target: Session, result: DeviceSegmentResult): DeviceReceipt = withContext(Dispatchers.IO) {
        check(local(target)) { "请连接原收件影库查看本机回执" }
        val api = provider.api(target)
        val id = requireNotNull(result.receipt)
        result.legacy?.let { binding ->
            check(binding.target == null || binding.target == target.instanceId) { "此回执属于其他本机影库" }
            val plan = api.getV2MigrationPlan(id).bodyOrThrow()
            val input = api.getV2Import(plan.importId).bodyOrThrow()
            check(plan.id == id && input.id == plan.importId && (binding.source == null || input.sourceId == binding.source) &&
                (binding.sha256 == null || input.sha256 == binding.sha256)) { "本机历史计划与此设备回执不符" }
            return@withContext DeviceReceipt.Legacy(id, plan.state.value, plan.matchedCount, plan.mediaCount,
                plan.skippedCount, plan.issueCount, plan.fingerprint, plan.checkpointId)
        }
        val binding = requireNotNull(result.native) { "此记录没有原生包身份" }
        check(binding.target == target.instanceId) { "请连接原收件影库查看本机回执" }
        val receipt = api.getNativeDelivery(id).bodyOrThrow()
        check(receipt.id == id) { "本机返回了其他回执" }
        DeviceSegments.checkBinding(receipt, binding)
        DeviceReceipt.Native(id, receipt.items.map { item ->
            val video = if (item.kind == NativeDeliveryKind.timelineMark) DeviceVideoBinding(item.sourceAssetId, item.sourceContentVersion,
                item.targetAssetId, item.targetContentVersion, item.contentSha256, item.contentBytes) else null
            DeviceReceiptItem(item.label, item.kind.value, item.block?.value, item.state.value, item.code, item.receiptId, video)
        })
    }
    suspend fun clear() = withContext(Dispatchers.IO) { lock.withLock {
        check(prefs.edit().remove("auth").putBoolean("background", false).remove("destination").commit()) { "未能清除设备授权" }
        WorkManager.getInstance(context).cancelUniqueWork("device-delivery-poll")
        notice("已清除本机设备令牌")
    } }
    private fun updatePolling(enabled: Boolean) {
        val manager = WorkManager.getInstance(context)
        if (enabled) {
            val work = PeriodicWorkRequestBuilder<DeviceDeliveryWorker>(15, TimeUnit.MINUTES).build()
            manager.enqueueUniquePeriodicWork("device-delivery-poll", ExistingPeriodicWorkPolicy.UPDATE, work)
        } else manager.cancelUniqueWork("device-delivery-poll")
    }
    fun schedule(target: Session, replaceAuthorProfiles: Boolean) {
        check(local(target)) { "请先连接本机影库，结果包将应用到此实例" }
        val source = requireNotNull(auth()?.takeIf { it.credentialId != null }) { "请先配置设备领取授权" }
        val input = workDataOf("manualInstance" to target.instanceId, "publisher" to source.publisher, "device" to source.device,
            "replaceAuthorProfiles" to replaceAuthorProfiles)
        val request = OneTimeWorkRequestBuilder<DeviceDeliveryWorker>().setInputData(input).build()
        WorkManager.getInstance(context).enqueueUniqueWork("device-delivery-now", ExistingWorkPolicy.KEEP, request)
    }
    internal suspend fun process(target: Session, request: Data): Unit = withContext(Dispatchers.IO) { lock.withLock {
        val requestedInstance = request.getString("manualInstance")
        val manual = requestedInstance != null
        if (manual && requestedInstance != target.instanceId) return@withLock
        if (!local(target) || (!manual && (!prefs.getBoolean("background",false) || prefs.getString("destination",null) != target.instanceId))) return@withLock
        val authorization = requireNotNull(auth()?.takeIf { it.credentialId != null }) { "请先配置设备领取授权" }
        val requestedPublisher = request.getString("publisher")
        if (manual && requestedPublisher != null && (requestedPublisher != authorization.publisher || request.getString("device") != authorization.device)) {
            notice("设备领取授权已更换，请按当前设置重新应用")
            return@withLock
        }
        val source = connection(authorization)
        val localApi = provider.api(target)
        val localId = localApi.getInstance().bodyOrThrow().id
        check(localId == target.instanceId) { "本机服务身份已改变，请重新核对设备目标" }
        var active = run()
        if (active == null) {
            // The service allows up to 32 live tasks per device; a package for another installation must stay pending there.
            val first = source.listDeviceApplyTasks(targetDeviceId = authorization.device, state = DeviceApplyState.pending, protocolVersion = 2, limit = 32)
                .bodyOrThrow().items.firstOrNull { it.segments.singleOrNull()?.targetInstanceId == target.instanceId }
                ?: run { if (manual) notice("目前没有发给本机影库的 Next 资料包"); return@withLock }
            DeviceSegments.nativeBinding(first, authorization.publisher, target.instanceId)
            check(backups.folder(target).tree != null) { "请先选择可写的维护目录，再领取资料包" }
            if (!backups.advance(target, manual)) { notice("正在准备本机两代备份，完成后领取"); throw WaitingForDeviceBackup() }
            val replaceProfiles = if (manual) request.getBoolean("replaceAuthorProfiles", false) else prefs.getBoolean("replaceAuthorProfiles", false)
            active = DeliveryRun(first.id, authorization.publisher, authorization.device, target.instanceId, replaceAuthorProfiles = replaceProfiles)
            saveRun(active)
        }
        var work = requireNotNull(active)
        check(work.destination == target.instanceId && work.device == authorization.device && work.publisher == authorization.publisher) { "未完成的领取属于原设备或实例" }
        var task = source.getDeviceApplyTask(work.task).bodyOrThrow()
        check(task.targetDeviceId == work.device) { "服务端包与已登记的设备不一致" }
        val binding = DeviceSegments.nativeBinding(task, work.publisher, work.destination)
        if (task.state.value == "pending") {
            task = source.claimDeviceApplyTask(task.id, operationKey("claim", work.publisher, task.id, work.device), RevisionRequest(task.revision)).bodyOrThrow()
        }
        if (task.state.value != "claimed") {
            if (task.state.value in setOf("failed", "partial", "applied")) cleanup(task, work)
            else notice("服务端任务已到终态 ${task.state.value}；请核对服务端与本机的实际回执")
            if (task.state.value in setOf("expired","cancelled")) { discardSegments(work); saveRun(null) }
            return@withLock
        }
        if (work.results.isEmpty()) {
            check(DeviceSegments.nativeBinding(task, work.publisher, work.destination) == binding) { "领取回执与原任务不一致" }
            val segment = task.segments.single()
            check(local(target) && sameTarget(target, settings.session.value)) { "本机实例已改变，待处理段保留" }
            val file = DeviceSegments.fetch(staging, work.publisher, task, segment, source)
            val latest = source.getDeviceApplyTask(task.id).bodyOrThrow()
            check(latest.state == task.state && latest.revision == task.revision && latest.segments == task.segments) { "领取已失效，停止应用后续段" }
            val result = try { DeviceSegments.applyNative(localApi, file, binding, work.replaceAuthorProfiles) }
            catch (failure: ApiFailure) {
                if (failure.status in setOf(400, 404, 409, 413, 415, 422))
                    ReceivedSegment(segment.kind.value,"skipped", code = failure.code?.takeIf { it.matches(Regex("[a-z0-9_]{1,80}")) } ?: "local_import_conflict")
                else throw failure
            }
            work = work.copy(results = listOf(result))
            saveRun(work)
        }
        val wroteLocally = work.results.any { it.state == "applied" || it.state == "partial" }
        if (wroteLocally && !backups.advance(target, manual)) { notice("已应用的段已保存，等待本机两代备份完成再回报"); throw WaitingForDeviceBackup() }
        val state = when {
            work.results.all { it.state == "applied" } -> DeviceApplyReport.State.applied
            wroteLocally -> DeviceApplyReport.State.partial
            else -> DeviceApplyReport.State.failed
        }
        val outcomes = work.results.map { it.toOutcome() }
        val result = source.reportDeviceApplyTask(task.id, operationKey("device-result",work.publisher,task.id,work.device),
            DeviceApplyReport(expectedRevision = task.revision, state = state, results = outcomes)).bodyOrThrow()
        cleanup(result, work)
    } }
    private fun cleanup(task: DeviceApplyTask, work: DeliveryRun) {
        check(task.id == work.task && task.results.orEmpty() == work.results.map { it.toOutcome() }) { "服务器尚未确认本机各段结果" }
        discardSegments(work); saveRun(null)
        notice(when (task.state.value) { "applied" -> "资料包已应用并取得服务端回执"; "partial" -> "资料包部分应用，未应用段请核对冲突"; else -> "资料包未应用，详情见最近任务" })
    }
    private fun discardSegments(work: DeliveryRun) {
        val file = File(staging, operationKey("segment",work.publisher,work.task,DeviceSegmentKind.nativeSelection.value))
        check(!file.exists() || file.delete()) { "已取得回执，设备暂存尚未清理" }
    }
    internal fun showFailure(failure: Exception) = notice(safeFailure(failure))
}

class DeviceDeliveryWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val graph = DataGraph.get(applicationContext)
        val target = graph.settings.session.value ?: return Result.success()
        return try { graph.deviceDelivery.process(target, inputData); Result.success() }
        catch (cancelled: kotlinx.coroutines.CancellationException) { throw cancelled }
        catch (_: WaitingForDeviceBackup) { Result.retry() }
        catch (problem: Exception) { graph.deviceDelivery.showFailure(problem)
            if (problem is java.io.IOException && (problem !is ApiFailure || problem.status >= 500 || problem.status == 429) && runAttemptCount < 5) Result.retry() else Result.failure() }
    }
}
