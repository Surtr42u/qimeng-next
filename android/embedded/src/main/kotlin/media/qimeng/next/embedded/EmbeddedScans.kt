package media.qimeng.next.embedded

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.Data
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import media.qimeng.next.runtime.*
import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID
import java.util.concurrent.TimeUnit

internal class EmbeddedScans(private val context: Context) : LocalScans {
    private val preferences = context.getSharedPreferences("embedded-scans", Context.MODE_PRIVATE)
    private val lock = Mutex()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val mutable = MutableStateFlow(read())
    override val state = mutable.asStateFlow()

    private fun read(): LocalScanState = try {
        val raw = preferences.getString("requests", "[]")!!
        check(raw.length <= 128 * 1024)
        val array = JSONArray(raw)
        check(array.length() <= 100)
        LocalScanState(preferences.getBoolean("charge-only", true), (0 until array.length()).map {
            val value = array.getJSONObject(it)
            LocalScanRequest(value.getString("id"), value.getString("instance"), value.getString("library"), value.getLong("revision"),
                value.getString("state"), value.optString("job").takeIf(String::isNotEmpty), value.getString("message"))
        })
    } catch (_: Exception) { LocalScanState(error = "本机待扫记录无法读取，已停止提交；原记录保留") }

    private suspend fun update(change: (LocalScanState) -> LocalScanState) = withContext(Dispatchers.IO) {
        lock.withLock {
            check(mutable.value.error == null) { mutable.value.error.orEmpty() }
            val next = change(mutable.value)
            val array = JSONArray()
            next.requests.forEach { item -> array.put(JSONObject().put("id", item.id).put("instance", item.instanceId)
                .put("library", item.libraryId).put("revision", item.expectedRevision).put("state", item.state)
                .put("job", item.jobId ?: "").put("message", item.message)) }
            if (!preferences.edit().putBoolean("charge-only", next.chargeOnly).putString("requests", array.toString()).commit()) {
                mutable.value = mutable.value.copy(error = "本机待扫记录未能保存，已停止提交；重新连接本机后重试读取")
                error("本机待扫记录未能保存，请重试")
            }
            mutable.value = next
        }
    }

    override suspend fun request(instanceId: String, libraryId: String, expectedRevision: Long) {
        var request: LocalScanRequest? = null
        update { current ->
            val previous = current.requests.find { it.instanceId == instanceId && it.libraryId == libraryId }
            request = previous?.takeIf { it.state in setOf("queued", "waiting") }
                ?: LocalScanRequest(UUID.randomUUID().toString(), instanceId, libraryId, expectedRevision)
            val remaining = current.requests.filterNot { it == previous }.toMutableList()
            // Accepted scans are already durable in Go. An old receipt must never fill the host queue forever.
            if (remaining.size == 100) remaining.firstOrNull { it.state == "submitted" }?.let(remaining::remove)
            check(remaining.size < 100) { "本机待扫影库已达上限，请先处理现有待扫任务" }
            current.copy(requests = remaining + request!!)
        }
        schedule(request!!, state.value.chargeOnly)
    }

    override suspend fun forget(instanceId: String, libraryId: String) {
        var removed: LocalScanRequest? = null
        update { state ->
            removed = state.requests.find { it.instanceId == instanceId && it.libraryId == libraryId }
            state.copy(requests = state.requests.filterNot { it == removed })
        }
        removed?.let { WorkManager.getInstance(context).cancelUniqueWork("qimeng-local-scan-${it.id}") }
    }

    override suspend fun setChargeOnly(enabled: Boolean) {
        update { it.copy(chargeOnly = enabled) }
        state.value.requests.filter { it.state in setOf("queued", "waiting") }.forEach { schedule(it, enabled, replace = true) }
    }

    override fun resume() {
        scope.launch {
            val current = lock.withLock {
                if (mutable.value.error != null) mutable.value = read()
                mutable.value
            }
            if (current.error == null) current.requests.filter { it.state in setOf("queued", "waiting") }
                .forEach { schedule(it, current.chargeOnly) }
        }
    }

    internal suspend fun result(id: String, state: String, message: String, jobId: String? = null) {
        update { it.copy(requests = it.requests.map { request ->
            if (request.id == id) request.copy(state = state, message = message, jobId = jobId ?: request.jobId) else request
        }) }
    }

    private fun schedule(request: LocalScanRequest, charging: Boolean, replace: Boolean = false) {
        val work = OneTimeWorkRequestBuilder<EmbeddedScanWorker>().setInputData(Data.Builder().putString("requestId", request.id).build())
            .setConstraints(Constraints.Builder().setRequiresCharging(charging).build())
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS).build()
        WorkManager.getInstance(context).enqueueUniqueWork("qimeng-local-scan-${request.id}",
            if (replace) ExistingWorkPolicy.REPLACE else ExistingWorkPolicy.KEEP, work)
    }
}

class EmbeddedScanWorker(context: Context, parameters: WorkerParameters) : CoroutineWorker(context, parameters) {
    override suspend fun doWork(): Result {
        val host = applicationContext as? LocalScanHost ?: return Result.failure()
        val scheduler = host.runtime.scans as? EmbeddedScans ?: return Result.failure()
        if (scheduler.state.value.error != null) return Result.failure()
        val request = scheduler.state.value.requests.find { it.id == inputData.getString("requestId") } ?: return Result.success()
        if (request.state !in setOf("queued", "waiting")) return Result.success()
        if (!host.runtime.shouldRestore) {
            scheduler.result(request.id, "waiting", "本机服务已停止；重新启用后继续待扫任务")
            return Result.success()
        }
        try {
            val connection = host.runtime.start()
            check(connection.instanceId == request.instanceId) { "待扫任务属于另一实例，已保留并停止提交" }
            val job = host.submitLocalScan(connection, request)
            scheduler.result(request.id, "submitted", "已提交，扫描进度由本机服务保存", job)
            return Result.success()
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: LocalScanFailure) {
            val retry = failure.retryable && runAttemptCount < 4
            scheduler.result(request.id, if (retry) "queued" else "failed", failure.message.orEmpty())
            return if (retry) Result.retry() else Result.success()
        } catch (_: Exception) {
            scheduler.result(request.id, "waiting", "本机服务暂不可用；请回到应用核对运行状态，原待扫任务已保留")
            return Result.success()
        }
    }
}
