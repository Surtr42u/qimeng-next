package media.qimeng.next.data

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.pm.ServiceInfo
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.work.CoroutineWorker
import androidx.work.ForegroundInfo
import androidx.work.WorkerParameters
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import media.qimeng.next.model.*
import media.qimeng.next.sdk.model.BatchCreate
import media.qimeng.next.sdk.model.EventCreate
import media.qimeng.next.sdk.model.UploadCreate
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile

class TransferWorker(context: Context, parameters: WorkerParameters) : CoroutineWorker(context, parameters) {
    private val graph = DataGraph.get(context)

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        try {
            if (graph.queue.state.value == null) graph.queue.load()
            val target = graph.settings.session.value ?: return@withContext Result.success()
            val api = graph.provider.api(target)
            val instance = api.getInstance().bodyOrThrow()
            if (instance.id != target.instanceId) return@withContext stopForTarget("此地址已更换服务器实例，请重新登录")
            var retry = false
            graph.queue.state.value!!.events.filter { sameTarget(it.target, target) && it.failure == null }.forEach { event ->
                try {
                    api.recordEvent(id = event.assetId, eventCreate = EventCreate(eventId = event.id,
                        kind = EventCreate.Kind.entries.first { it.value == event.kind }, contentVersion = event.contentVersion,
                        positionMs = event.positionMs, sessionId = event.sessionId, seconds = event.seconds, occurredAt = event.occurredAt)).bodyOrThrow()
                    graph.queue.update { it.copy(events = it.events.filterNot { item -> item.id == event.id }) }
                } catch (cancelled: CancellationException) { throw cancelled }
                catch (failure: ApiFailure) {
                    if (failure.status == 404 || failure.status == 409) {
                        graph.queue.update { snapshot -> snapshot.copy(events = snapshot.events.map {
                            if (it.id == event.id) it.copy(failure = failure.message) else it
                        }) }
                    } else throw failure
                } catch (_: IOException) { retry = true }
            }
            val pending = graph.queue.state.value!!.transfers.filter { it.state in setOf(TransferState.QUEUED, TransferState.UPLOADING) }
            if (pending.isNotEmpty()) setForeground(notification())
            pending.forEach { task ->
                if (!sameTarget(task.target, graph.settings.session.value)) {
                    update(task.id) { it.copy(state = TransferState.WAITING, message = "请连接任务原来的服务器") }
                    return@forEach
                }
                try { transfer(task) }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (failure: Exception) {
                    val transient = failure is IOException && (failure !is ApiFailure || failure.status == 429 || failure.status >= 500)
                    val automatic = transient && runAttemptCount < 4
                    update(task.id) { it.copy(state = if (automatic) TransferState.QUEUED else TransferState.FAILED,
                        message = if (automatic) "连接中断，等待自动重试并核对进度" else safeFailure(failure)) }
                    retry = retry || automatic
                }
            }
            if (retry) Result.retry() else Result.success()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: Exception) {
            // Durable records remain unchanged; never clear unsent events on a transport failure.
            graph.queue.reportProblem(safeFailure(failure))
            if (failure is ApiFailure && failure.status in setOf(401, 403)) Result.success() else Result.retry()
        }
    }

    private suspend fun transfer(initial: Transfer) {
        val api = graph.provider.api(initial.target)
        if (current(initial.id)?.state == TransferState.CANCELLED) return
        update(initial.id) { it.copy(state = TransferState.UPLOADING, message = "核对传输进度") }
        var task = current(initial.id) ?: return
        if (task.batchId == null) {
            val batch = api.createBatch(idempotencyKey = "batch-${task.batchKey}", batchCreate = BatchCreate(libraryId = task.libraryId,
                mode = BatchCreate.Mode.entries.first { it.value == task.mode.name.lowercase() }, label = task.batchLabel)).bodyOrThrow()
            update(task.id) { it.copy(batchId = batch.id) }
            task = current(task.id) ?: return
        }
        if (isStopped || task.state == TransferState.CANCELLED) return
        var upload = if (task.uploadId == null) {
            api.createUpload(idempotencyKey = "upload-${task.id}", uploadCreate = UploadCreate(libraryId = task.libraryId,
                fileName = task.name, propertySize = task.bytes, mimeType = task.mimeType,
                mode = UploadCreate.Mode.entries.first { it.value == task.mode.name.lowercase() }, batchId = task.batchId,
                sourceMtime = task.sourceMtime, relativeDirectory = task.relativeDirectory)).bodyOrThrow()
        } else api.getUpload(task.uploadId!!).bodyOrThrow()
        update(task.id) { it.copy(uploadId = upload.id, offset = upload.offset) }
        if (isStopped || current(task.id)?.state == TransferState.CANCELLED) return
        if (upload.state.value in setOf("ingested", "staged")) {
            finish(task.id, upload.assetId, upload.state.value)
            return
        }
        check(upload.state.value == "receiving") { upload.errorMessage ?: "服务器已停止此传输，请在任务报告核对" }
        val file = File(applicationContext.filesDir, "upload-spool/${task.spoolName}")
        check(file.isFile && file.length() == task.bytes) { "本机传输副本缺失或变化，不能继续原任务" }
        RandomAccessFile(file, "r").use { source ->
            while (upload.offset < task.bytes) {
                if (isStopped || current(task.id)?.state == TransferState.CANCELLED) return
                val chunk = ByteArray(minOf(1024 * 1024L, task.bytes - upload.offset).toInt())
                source.seek(upload.offset)
                source.readFully(chunk)
                upload = api.appendUpload(id = upload.id, uploadOffset = upload.offset,
                    body = chunk.toRequestBody("application/octet-stream".toMediaType())).bodyOrThrow()
                update(task.id) { it.copy(offset = upload.offset, message = "正在上传") }
            }
        }
        if (current(task.id)?.state == TransferState.CANCELLED) return
        val completed = api.completeUpload(id = upload.id, idempotencyKey = "complete-${task.id}").bodyOrThrow()
        check(completed.upload.state.value in setOf("ingested", "staged")) { completed.upload.errorMessage ?: "尚未入库，请查看任务报告" }
        finish(task.id, completed.upload.assetId, completed.upload.state.value)
    }

    private suspend fun finish(id: String, assetId: String?, state: String) {
        graph.queue.update { snapshot -> snapshot.copy(transfers = snapshot.transfers.map {
            if (it.id == id) it.copy(state = TransferState.SUCCEEDED, assetId = assetId, offset = it.bytes,
                message = if (state == "staged") "已传输，等待智能整理；来源文件保留" else "已入库；来源文件保留") else it
        }) }
        current(id)?.let { File(applicationContext.filesDir, "upload-spool/${it.spoolName}").delete() }
    }

    private fun current(id: String) = graph.queue.state.value?.transfers?.find { it.id == id }
    private suspend fun update(id: String, transform: (Transfer) -> Transfer) {
        graph.queue.update { snapshot -> snapshot.copy(transfers = snapshot.transfers.map {
            if (it.id != id) it else {
                val updated = transform(it)
                if (it.state == TransferState.CANCELLED) updated.copy(state = it.state, message = it.message) else updated
            }
        }) }
    }

    private suspend fun stopForTarget(message: String): Result {
        graph.queue.update { it.copy(transfers = it.transfers.map { item ->
            if (item.state in setOf(TransferState.QUEUED, TransferState.UPLOADING)) item.copy(state = TransferState.WAITING, message = message) else item
        }) }
        return Result.success()
    }

    private fun notification(): ForegroundInfo {
        val manager = applicationContext.getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel("uploads", "媒体传输", NotificationManager.IMPORTANCE_LOW))
        val notification = NotificationCompat.Builder(applicationContext, "uploads").setSmallIcon(android.R.drawable.stat_sys_upload)
            .setContentTitle("绮梦媒体传输").setContentText("正在上传；可在应用内查看进度或停止任务")
            .setOngoing(true).build()
        return if (Build.VERSION.SDK_INT >= 29) ForegroundInfo(7001, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        else ForegroundInfo(7001, notification)
    }
}

internal fun safeFailure(failure: Exception): String = when (failure) {
    is ApiFailure -> failure.message
    is IOException -> "连接失败，原任务与进度已保留"
    is IllegalArgumentException, is IllegalStateException -> failure.message ?: "操作未完成，请重试"
    else -> "操作未完成，请重试"
}
