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
import media.qimeng.next.sdk.model.BatchInputsSet
import media.qimeng.next.sdk.model.BatchInputCancel
import media.qimeng.next.sdk.model.IntakeInput
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
            if (inputData.getString("targetKey") != transferTargetKey(target)) return@withContext Result.success()
            // The journal is durable; an embedded startup deferral will be re-enqueued after admission.
            if (!requireNotNull(graph.queue.state.value).hasReadyTransferWork(target) ||
                !transferConnectionReady(applicationContext, target)) return@withContext Result.success()
            val api = graph.provider.api(target)
            val instance = try { api.getInstance().bodyOrThrow() }
            catch (failure: IOException) {
                if (failure is ApiFailure) throw failure
                if (sameTarget(target, graph.settings.session.value)) graph.queue.reportTransportProblem(target, safeFailure(failure))
                return@withContext Result.retry()
            }
            if (instance.id != target.instanceId) return@withContext stopForTarget("此地址已更换服务器实例，请重新登录")
            graph.queue.clearTransportProblem(target)
            val initial = requireNotNull(graph.queue.state.value)
            val withTransfers = initial.transfers.mapTo(mutableSetOf(), Transfer::batchKey)
            var retry = false
            // Reconcile seals written by older clients even if all their local rows are failed or already finished.
            initial.batches.filter { sameTarget(it.target, target) && !it.inputsChecked && (it.manifestFingerprint != null || it.key !in withTransfers) }
                .forEach { batch ->
                    try { prepareBatch(batch.key) }
                    catch (cancelled: CancellationException) { throw cancelled }
                    catch (failure: Exception) {
                        graph.queue.reportProblem(safeFailure(failure))
                        if (failure is IOException && (failure !is ApiFailure || failure.status == 429 || failure.status >= 500)) retry = true
                    }
                }
            retry = graph.activityEvents.flush(target, api) || retry
            val pending = graph.queue.state.value!!.transfers.filter { ownsTarget(it.target) &&
                (it.state in setOf(TransferState.QUEUED, TransferState.UPLOADING) || (it.state == TransferState.CANCELLED && !it.cancelConfirmed)) }
            if (pending.isNotEmpty()) setForeground(notification())
            pending.forEach { pendingTask ->
                val task = current(pendingTask.id) ?: return@forEach
                if (task.state == TransferState.REJECTED) return@forEach
                if (task.state == TransferState.CANCELLED) {
                    if (!sameTarget(task.target, graph.settings.session.value)) return@forEach
                    try { confirmCancellation(task) }
                    catch (cancelled: CancellationException) { throw cancelled }
                    catch (failure: Exception) {
                        if (current(task.id)?.let { !ownsTarget(it.target) } == true) return@forEach
                        graph.queue.update { snapshot -> snapshot.copy(transfers = snapshot.transfers.map {
                            if (it.id == task.id && it.state == TransferState.CANCELLED) it.copy(message = "本机已停止发送；服务器取消尚未确认，连接后继续核对。${safeFailure(failure)}") else it
                        }) }
                        if (runAttemptCount < 4 && (failure !is ApiFailure || failure.status == 409 || failure.status == 429 || failure.status >= 500)) retry = true
                    }
                    return@forEach
                }
                if (collectionPredecessor(task, graph.queue.state.value!!.transfers) != null) {
                    update(task.id) { it.copy(message = "等待同一集合前面的文件完成；请先处理前项失败或明确停止前项") }
                    return@forEach
                }
                if (!sameTarget(task.target, graph.settings.session.value)) {
                    graph.queue.update { snapshot -> snapshot.copy(transfers = snapshot.transfers.map {
                        if (it.id == task.id && !sameTarget(it.target, graph.settings.session.value) && it.state != TransferState.CANCELLED)
                            it.copy(state = TransferState.WAITING, message = "请连接任务原来的服务器") else it
                    }) }
                    return@forEach
                }
                try { transfer(task) }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (failure: Exception) {
                    if (current(task.id)?.let { !ownsTarget(it.target) } == true) return@forEach
                    if (current(task.id)?.state == TransferState.SUCCEEDED) {
                        graph.queue.reportProblem("服务器结果已保存，本机副本清理未完成；不会重新上传。")
                        return@forEach
                    }
                    val transient = failure is IOException && (failure !is ApiFailure || failure.status == 429 || failure.status >= 500)
                    val automatic = transient && runAttemptCount < 4
                    update(task.id) { it.copy(state = if (automatic) TransferState.QUEUED else TransferState.FAILED,
                        message = if (automatic) "连接中断，等待自动重试并核对进度" else safeFailure(failure)) }
                    retry = retry || automatic
                }
            }
            val awaitingArchive = graph.archiveProtection.run(target)
            if (retry || awaitingArchive) Result.retry() else Result.success()
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
        if (current(initial.id)?.state?.isFinished() != false) return
        update(initial.id) { it.copy(state = TransferState.UPLOADING, message = "核对传输进度") }
        var task = current(initial.id) ?: return
        if (!ownsTarget(task.target)) return
        prepareBatch(task.batchKey)
        task = current(task.id) ?: return
        if (isStopped || task.state.isFinished() || !ownsTarget(task.target)) return
        check(task.sourceHash?.length == 64) { "传输快照缺少完整摘要，请重新选择文件" }
        var upload = if (task.uploadId == null) {
            api.createUpload(idempotencyKey = "upload-${task.id}", uploadCreate = UploadCreate(libraryId = task.libraryId,
                fileName = task.name, propertySize = task.bytes, mimeType = task.mimeType,
                mode = UploadCreate.Mode.entries.first { it.value == task.mode.name.lowercase() }, batchId = task.batchId,
                sourceMtime = task.sourceMtime, relativeDirectory = task.relativeDirectory, authorIds = task.options.authorIds,
                collectionId = task.options.collectionId, note = task.options.note, acquisitionSources = task.options.acquisitionSources,
                authorAcquisitionSources = task.options.authorAcquisitionSources.takeIf { it.isNotEmpty() },
                family = task.options.family,
                expectedHash = task.sourceHash, originalFileName = task.originalName, inputId = task.id)).bodyOrThrow()
        } else api.getUpload(task.uploadId!!).bodyOrThrow()
        update(task.id) { it.copy(uploadId = upload.id, offset = upload.offset) }
        if (isStopped || current(task.id)?.state == TransferState.CANCELLED) return
        if (upload.state.value == "ingested" || (upload.state.value == "staged" && task.mode == UploadMode.INTELLIGENT)) {
            finish(task.id, upload.assetId, upload.state.value)
            return
        }
        check(upload.state.value == "receiving" || (upload.state.value == "staged" && upload.offset == task.bytes)) {
            upload.errorMessage ?: "服务器已停止此传输，请在任务报告核对"
        }
        if (upload.offset < task.bytes) {
            val file = File(applicationContext.filesDir, "upload-spool/${task.spoolName}")
            check(file.isFile && file.length() == task.bytes) { "本机传输副本缺失或变化，不能继续原任务" }
            RandomAccessFile(file, "r").use { source ->
                while (upload.offset < task.bytes) {
                    if (isStopped || current(task.id)?.let { it.state == TransferState.CANCELLED || !ownsTarget(it.target) } != false) return
                    val chunk = ByteArray(minOf(1024 * 1024L, task.bytes - upload.offset).toInt())
                    source.seek(upload.offset)
                    source.readFully(chunk)
                    upload = api.appendUpload(id = upload.id, uploadOffset = upload.offset,
                        body = chunk.toRequestBody("application/octet-stream".toMediaType())).bodyOrThrow()
                    update(task.id) { it.copy(offset = upload.offset, message = "正在上传") }
                }
            }
        }
        if (current(task.id)?.state == TransferState.CANCELLED) return
        val completed = api.completeUpload(id = upload.id, idempotencyKey = "complete-${task.id}").bodyOrThrow()
        check(completed.upload.state.value == "ingested" || (completed.upload.state.value == "staged" && task.mode == UploadMode.INTELLIGENT)) {
            completed.upload.errorMessage ?: "服务器尚未确认普通上传入库，请重试核对原任务"
        }
        finish(task.id, completed.upload.assetId, completed.upload.state.value)
    }

    /** The entire immutable picker manifest is committed before the first upload of this batch. */
    private suspend fun prepareBatch(key: String): TransferBatch {
        var batch = graph.queue.state.value!!.batches.find { it.key == key }
            ?: error("旧传输记录缺少完整原始清单，请保留来源并重新选择文件")
        if (!ownsTarget(batch.target)) throw CancellationException("任务已交给新端点连接")
        val api = graph.provider.api(batch.target)
        if (batch.serverId == null) {
            val created = api.createBatch(idempotencyKey = "batch-${batch.key}", batchCreate = BatchCreate(libraryId = batch.libraryId,
                mode = BatchCreate.Mode.entries.first { it.value == batch.mode.name.lowercase() }, label = batch.label)).bodyOrThrow()
            graph.queue.update { snapshot -> snapshot.copy(
                batches = snapshot.batches.map { if (it.key == batch.key) it.copy(serverId = created.id) else it },
                transfers = snapshot.transfers.map { if (it.batchKey == batch.key) it.copy(batchId = created.id) else it }) }
            batch = graph.queue.state.value!!.batches.first { it.key == batch.key }
        }
        if (batch.inputsChecked) return batch
        val serverId = requireNotNull(batch.serverId)
        val fingerprint = batch.manifestFingerprint ?: run {
            val sealed = api.setBatchInputs(id = serverId, idempotencyKey = "manifest-${batch.key}",
                batchInputsSet = BatchInputsSet(expectedManifestRevision = 1, inputs = batch.inputs.map {
                    IntakeInput(id = it.id, originalFileName = it.originalFileName,
                        nameState = IntakeInput.NameState.entries.first { state -> state.value == it.nameState },
                        propertySize = it.size, expectedHash = it.expectedHash)
                })).bodyOrThrow()
            check(sealed.manifestState.value == "sealed" && sealed.manifestFingerprint.isNotBlank()) { "服务器未封存原始输入清单" }
            sealed.manifestFingerprint
        }
        val report = api.getBatch(serverId).bodyOrThrow()
        check(report.batch.manifestFingerprint == fingerprint && report.inputs.map { it.id } == batch.inputs.map { it.id }) {
            "服务器报告与原始输入清单不一致，已停止传输"
        }
        val rejected = report.inputs.mapNotNull { input -> input.intakeFailure()?.let { input.id to it } }.toMap()
        graph.queue.update { snapshot -> snapshot.copy(
            batches = snapshot.batches.map { if (it.key == key && ownsTarget(it.target)) it.copy(manifestFingerprint = fingerprint, inputsChecked = true) else it },
            transfers = snapshot.transfers.map { task ->
                val reason = rejected[task.id]
                if (task.batchKey == key && ownsTarget(task.target) && reason != null) task.copy(state = TransferState.REJECTED, message = reason) else task
            },
            intakeFailures = snapshot.intakeFailures.map { failure ->
                val reason = rejected[failure.id]
                if (failure.batchKey == key && reason != null) failure.copy(message = reason) else failure
            }) }
        return graph.queue.state.value!!.batches.first { it.key == key }
    }

    private suspend fun confirmCancellation(task: Transfer) {
        val batch = prepareBatch(task.batchKey)
        if (current(task.id)?.state != TransferState.CANCELLED) return
        val api = graph.provider.api(task.target)
        try {
            val result = api.cancelBatchInput(id = requireNotNull(batch.serverId), inputId = task.id,
                idempotencyKey = "cancel-input-${task.id}", batchInputCancel = BatchInputCancel(requireNotNull(batch.manifestFingerprint))).bodyOrThrow()
            check(result.state.value == "cancelled") { "服务器尚未确认取消此输入" }
            graph.queue.update { snapshot -> snapshot.copy(transfers = snapshot.transfers.map {
                if (it.id == task.id && it.state == TransferState.CANCELLED) it.copy(cancelConfirmed = true, message = "已停止；原始输入清单保留取消记录，来源文件保留") else it
            }) }
        } catch (failure: ApiFailure) {
            if (failure.status == 409) {
                val detail = api.getBatch(requireNotNull(batch.serverId)).bodyOrThrow()
                val input = detail.inputs.first { it.id == task.id }
                if (input.state.value == "ingested") {
                    finish(task.id, input.assetId, "ingested")
                    return
                }
            }
            throw failure
        }
    }

    private suspend fun finish(id: String, assetId: String?, state: String) {
        check(state != "ingested" || !assetId.isNullOrBlank()) { "服务器尚未返回入库作品身份，请重试核对原任务" }
        graph.queue.update { snapshot -> snapshot.copy(transfers = snapshot.transfers.map {
            if (it.id == id && ownsTarget(it.target)) it.copy(state = TransferState.SUCCEEDED, assetId = assetId, offset = it.bytes,
                message = if (state == "staged") "已传输，等待智能整理；来源文件保留" else "已入库；来源文件保留") else it
        }) }
        current(id)?.takeIf { it.state == TransferState.SUCCEEDED && ownsTarget(it.target) }?.let { task ->
            if (state == "ingested") graph.uploads.recordPublication(task)
            val file = File(applicationContext.filesDir, "upload-spool/${task.spoolName}")
            if (file.exists() && !file.delete() && file.exists()) {
                update(id) { it.copy(message = "${it.message}；本机传输副本尚未清理，入库结果保留") }
            }
        }
    }

    private fun current(id: String) = graph.queue.state.value?.transfers?.find { it.id == id }
    private suspend fun update(id: String, transform: (Transfer) -> Transfer) {
        graph.queue.update { snapshot -> snapshot.copy(transfers = snapshot.transfers.map {
            if (it.id != id || !ownsTarget(it.target)) it else {
                val updated = transform(it)
                if (it.state == TransferState.CANCELLED) updated.copy(state = it.state, message = it.message) else updated
            }
        }) }
    }

    private fun ownsTarget(target: Session) = inputData.getString("targetKey") == transferTargetKey(target)

    private suspend fun stopForTarget(message: String): Result {
        graph.queue.update { it.copy(transfers = it.transfers.map { item ->
            if (ownsTarget(item.target) && item.state in setOf(TransferState.QUEUED, TransferState.UPLOADING)) item.copy(state = TransferState.WAITING, message = message) else item
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
