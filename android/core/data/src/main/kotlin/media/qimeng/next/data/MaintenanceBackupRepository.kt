package media.qimeng.next.data

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.DocumentsContract
import androidx.work.*
import com.google.gson.Gson
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import media.qimeng.next.model.MaintenanceFolder
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget
import java.util.UUID
import java.util.concurrent.TimeUnit

private data class Rotation(val token: String, val checkpoint: String? = null, val exportKey: String = UUID.randomUUID().toString(),
    val masterHash: String? = null, val old: BackupGeneration? = null, val older: BackupGeneration? = null,
    val prepared: BackupGeneration? = null, val phase: String = "prepare")

class MaintenanceBackupRepository internal constructor(private val context: Context, private val provider: ApiProvider,
    private val settings: SecureSettings, private val archives: ArchiveRepository) {
    private val storage = context.getSharedPreferences("maintenance-folders", Context.MODE_PRIVATE)
    private val gson = Gson()
    private val lock = Mutex()
    private val mutable = MutableStateFlow(0L)
    val changes = mutable.asStateFlow()
    private fun key(target: Session) = "instance-${target.instanceId}"
    fun folder(target: Session): MaintenanceFolder = storage.getString(key(target), null)?.let { gson.fromJson(it, MaintenanceFolder::class.java) } ?: MaintenanceFolder()
    private fun job(target: Session): Rotation? = storage.getString("${key(target)}-job", null)?.let { gson.fromJson(it, Rotation::class.java) }
    private fun save(target: Session, binding: MaintenanceFolder, job: Rotation? = job(target)) {
        check(storage.edit().putString(key(target), gson.toJson(binding.copy(pending = job != null)))
            .putString("${key(target)}-job", job?.let { gson.toJson(it) }).commit()) { "维护目录状态未能保存" }
        mutable.update { it + 1 }
    }
    suspend fun bind(target: Session, uri: Uri, flags: Int, abandonPrevious: Boolean = false) = withContext(Dispatchers.IO) {
        lock.withLock {
            require(uri.scheme == "content" && DocumentsContract.isTreeUri(uri)) { "请用系统目录选择器授权" }
            val access = Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION
            require((flags and access) == access) { "维护目录需要读写授权" }
            var binding = folder(target)
            if (binding.pending && binding.tree != uri.toString()) {
                check(abandonPrevious) { "请先完成上次维护目录轮换" }
                binding = abandonLocked(target, binding)
            }
            val tree = MaintenanceTree(context.contentResolver, uri)
            val current = tree.generation("当前")
            val older = tree.generation("上次")
            check(listOfNotNull(current, older).all { it.instanceId == target.instanceId }) { "该目录属于另一影库，请选择对应目录" }
            check(sameTarget(target, settings.session.value)) { "连接已改变，请重新选择维护目录" }
            val archive = tree.directory(tree.root, "上传归档")
            runCatching { tree.overwriteText(tree.root, maintenanceReadmeName, maintenanceReadme) }
            val label = DocumentsContract.getTreeDocumentId(uri).substringAfterLast('/').substringAfterLast(':')
            check(sameTarget(target, settings.session.value)) { "连接已改变，请重新选择维护目录" }
            val resolver = context.contentResolver
            val held = resolver.persistedUriPermissions.find { it.uri == uri }
            val newlyGranted = access and ((if (held?.isReadPermission == true) Intent.FLAG_GRANT_READ_URI_PERMISSION else 0) or
                (if (held?.isWritePermission == true) Intent.FLAG_GRANT_WRITE_URI_PERMISSION else 0)).inv()
            resolver.takePersistableUriPermission(uri, access)
            try {
                save(target, binding.copy(tree = uri.toString(), label = label, lastChecked = 0))
            } catch (failure: Exception) {
                if (newlyGranted != 0) try { resolver.releasePersistableUriPermission(uri, newlyGranted) }
                catch (release: SecurityException) { failure.addSuppressed(release) }
                throw failure
            }
            archives.selectMaintenanceArchive(archive)
            val previousTree = binding.tree
            if (previousTree != null && previousTree != uri.toString() && !referenced(previousTree) && !archives.store.needsGrant(previousTree)) {
                val oldUri = Uri.parse(previousTree)
                if (context.contentResolver.persistedUriPermissions.any { it.uri == oldUri }) context.contentResolver.releasePersistableUriPermission(oldUri, access)
            }
        }
        schedule(target, manual = true)
    }

    /** 放弃当前实例未完成的轮换；只清理暂存与标记，保留已有「当前 / 上次」与上传归档。 */
    suspend fun abandon(target: Session) = withContext(Dispatchers.IO) {
        lock.withLock { abandonLocked(target, folder(target)) }
    }

    private suspend fun abandonLocked(target: Session, binding: MaintenanceFolder): MaintenanceFolder {
        val work = job(target)
        val raw = binding.tree
        if (raw != null && work != null) {
            val tree = MaintenanceTree(context.contentResolver, Uri.parse(raw))
            val marker = org.json.JSONObject().put("instanceId", target.instanceId).put("token", work.token).toString()
            tree.find(tree.root, mobileRotationMarker)?.let {
                if (tree.smallText(it.uri) == marker) tree.remove(it.uri)
            }
            runCatching { tree.discardStage(work.token) }
            tree.dropReusableFolder()
            runCatching { tree.overwriteText(tree.root, maintenanceReadmeName, maintenanceReadme) }
        }
        save(target, binding.copy(message = "已放弃上次未完成的轮换"), null)
        return folder(target)
    }

    internal fun referenced(raw: String): Boolean = storage.all.any { (key, value) -> key.startsWith("instance-") && !key.endsWith("-job") &&
        value is String && gson.fromJson(value, MaintenanceFolder::class.java).tree == raw }
    suspend fun automatic(target: Session, enabled: Boolean) = withContext(Dispatchers.IO) {
        lock.withLock { val record = folder(target); check(record.tree != null) { "请先设置维护目录" }; save(target, record.copy(automatic = enabled)) }
        if (enabled) resume(target)
    }
    fun resume(target: Session) {
        val state = folder(target)
        if (state.pending || (state.automatic && System.currentTimeMillis() - state.lastChecked >= TimeUnit.DAYS.toMillis(1))) schedule(target, manual = false)
    }
    fun schedule(target: Session, manual: Boolean) {
        check(folder(target).tree != null) { "请先设置维护目录" }
        val request = OneTimeWorkRequestBuilder<MaintenanceBackupWorker>().setInputData(workDataOf("instance" to target.instanceId, "manual" to manual))
            .setBackoffCriteria(BackoffPolicy.LINEAR, 12, TimeUnit.SECONDS).build()
        WorkManager.getInstance(context).enqueueUniqueWork("maintenance-backup-${target.instanceId}",
            if (manual) ExistingWorkPolicy.APPEND_OR_REPLACE else ExistingWorkPolicy.KEEP, request)
    }

    /** One pass returns false while Go's durable export is queued or running. */
    internal suspend fun advance(target: Session, manual: Boolean): Boolean = withContext(Dispatchers.IO) { lock.withLock {
        val binding = folder(target)
        val raw = binding.tree ?: error("维护目录已解除绑定")
        val tree = MaintenanceTree(context.contentResolver, Uri.parse(raw))
        val api = provider.api(target)
        val anchor = api.getBackupChangeState().bodyOrThrow()
        check(anchor.instanceId == target.instanceId) { "服务器身份已改变" }
        if (tree.find(tree.root, webRotationMarker)?.let { tree.smallText(it.uri).isNotEmpty() } == true) error("请先在浏览器完成未结束的归档")
        var work = job(target)
        if (work == null) {
            check(tree.find(tree.root, mobileRotationMarker) == null) { "目录中有其他手机未结束的轮换" }
            val current = tree.generation("当前")
            check(current == null || current.instanceId == target.instanceId) { "该目录属于另一影库" }
            if (current?.businessRevision == anchor.businessRevision) {
                if (manual) tree.verify("当前", current)
                save(target, binding.copy(lastChecked = System.currentTimeMillis(), message = "当前资料已是最新一代。"), null)
                return@withLock true
            }
            val older = tree.generation("上次")
            check(older == null || (current != null && older.instanceId == target.instanceId)) { "维护目录中的两代资料不匹配" }
            work = Rotation(UUID.randomUUID().toString(), old = current, older = older)
            save(target, binding.copy(message = "正在建立本次维护检查点"), work)
        }
        var active = requireNotNull(work)
        val marker = org.json.JSONObject().put("instanceId", target.instanceId).put("token", active.token).toString()
        tree.writeText(tree.root, mobileRotationMarker, marker)
        val stage = ".绮梦手机暂存-${active.token}"
        val retired = ".绮梦手机旧代-${active.token}"
        if (active.prepared == null) {
            var checkpoint = if (active.checkpoint == null) api.createBackupManifest("backup-${active.token}").bodyOrThrow()
                else api.getBackup(requireNotNull(active.checkpoint)).bodyOrThrow()
            val revision = requireNotNull(checkpoint.businessRevision) { "服务端未返回本份快照的业务修订" }
            if (active.checkpoint == null) { active = active.copy(checkpoint = checkpoint.id); save(target, binding, active) }
            var masterHash = active.masterHash
            if (masterHash == null) {
                if (revision != anchor.businessRevision) {
                    tree.find(tree.root, mobileRotationMarker)?.let { tree.remove(it.uri) }
                    save(target, binding.copy(message = "资料在建立快照时有变化，将重新捕获同一代次"), null)
                    return@withLock false
                }
                val directory = tree.staging(stage)
                // The readable master is a live projection and must be bracketed by the checkpoint revision.
                val master = api.exportMasterAuthorList().bodyOrThrow().use { it.byteStream().use { input -> tree.part(directory, "master", input) } }
                val after = api.getBackupChangeState().bodyOrThrow()
                check(after.instanceId == target.instanceId) { "维护期间服务器身份发生改变" }
                if (after.businessRevision != revision) {
                    tree.discardMasterStage(stage, master)
                    tree.find(tree.root, mobileRotationMarker)?.let { tree.remove(it.uri) }
                    save(target, binding.copy(message = "读取作者总表时资料有变化，将重新捕获快照"), null)
                    return@withLock false
                }
                active = active.copy(masterHash = master); masterHash = master; save(target, binding, active)
            }
            val directory = tree.staging(stage)
            if (checkpoint.exportState?.value in setOf("failed", "interrupted")) {
                check(manual) { "完整归档制作中断，可点击导出备份重试" }
                active = active.copy(exportKey = UUID.randomUUID().toString()); save(target, binding, active)
                checkpoint = api.exportBackup(checkpoint.id, active.exportKey).bodyOrThrow()
            } else if (checkpoint.exportState?.value == "not_started") checkpoint = api.exportBackup(checkpoint.id, active.exportKey).bodyOrThrow()
            if (checkpoint.exportState?.value in setOf("queued", "running")) {
                save(target, binding.copy(message = "服务器正在准备完整归档（${checkpoint.exportedFiles ?: 0}/${checkpoint.totalFiles ?: 0}）"), active)
                return@withLock false
            }
            check(checkpoint.exportState?.value == "completed" && !checkpoint.archiveHash.isNullOrEmpty()) { "完整归档尚未准备好，原有两代保留" }
            val report = api.exportBackupAuthorDocuments(checkpoint.id).bodyOrThrow().use { it.byteStream().use { input -> tree.part(directory, "report", input) } }
            val index = "[\"作者总表.txt\"]".byteInputStream().use { tree.part(directory, "index", it) }
            val archive = api.downloadBackupArchive(checkpoint.id).bodyOrThrow().use { it.byteStream().use { input -> tree.part(directory, "archive", input, checkpoint.archiveHash) } }
            val next = BackupGeneration(target.instanceId, revision, checkpoint.id, mapOf("master" to requireNotNull(masterHash), "report" to report, "index" to index, "archive" to archive))
            tree.seal(directory, next)
            active.old?.let { tree.verify("当前", it) }; active.older?.let { tree.verify("上次", it) }
            active = active.copy(prepared = next, phase = "retire"); save(target, binding, active)
        }
        val prepared = requireNotNull(active.prepared)
        fun moved(from: String, to: String, expected: BackupGeneration) {
            val targetGeneration = tree.generation(to)
            if (targetGeneration == expected && tree.find(tree.root, from) == null) return
            check(targetGeneration == null && tree.generation(from) == expected) { "目录轮换过程中代次发生变化，已保留现场" }
            tree.rename(from, to)
        }
        if (active.phase == "retire") {
            active.older?.let { moved("上次", retired, it) }
            active = active.copy(phase = "previous"); save(target, binding, active)
        }
        if (active.phase == "previous") {
            active.old?.let { moved("当前", "上次", it) }
            active = active.copy(phase = "current"); save(target, binding, active)
        }
        if (active.phase == "current") {
            moved(stage, "当前", prepared)
            tree.verify("当前", prepared)
            active = active.copy(phase = "cleanup"); save(target, binding, active)
        }
        active.older?.let { tree.removeExpired(retired, it) }
        tree.find(tree.root, mobileRotationMarker)?.let { check(tree.smallText(it.uri) == marker); tree.remove(it.uri) }
        tree.dropReusableFolder()
        runCatching { tree.overwriteText(tree.root, maintenanceReadmeName, maintenanceReadme) }
        save(target, binding.copy(lastChecked = System.currentTimeMillis(), message = "当前备份与作者资料已更新。"), null)
        true
    } }

    internal suspend fun failure(target: Session, text: String) = lock.withLock { save(target, folder(target).copy(message = text)) }
    suspend fun currentAuthorFile(target: Session): Uri? = withContext(Dispatchers.IO) {
        val raw = folder(target).tree ?: return@withContext null
        val tree = MaintenanceTree(context.contentResolver, Uri.parse(raw))
        if (tree.find(tree.root, webRotationMarker) != null || tree.find(tree.root, mobileRotationMarker) != null) error("资料仍在轮换，请待保存结束后读取")
        val recorded = tree.generation("当前") ?: return@withContext null
        check(recorded.instanceId == target.instanceId) { "该维护目录属于另一影库" }
        val dir = tree.find(tree.root, "当前")?.uri ?: return@withContext null
        val sub = tree.find(dir, "作者资料")?.uri ?: return@withContext null
        val file = tree.find(sub, "作者总表.txt")?.uri ?: return@withContext null
        check(tree.hash(file) == recorded.hashes["master"]) { "作者总表摘要不匹配" }; file
    }
}
