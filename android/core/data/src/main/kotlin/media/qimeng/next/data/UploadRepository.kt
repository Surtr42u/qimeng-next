package media.qimeng.next.data

import android.content.Context
import android.net.Uri
import android.provider.OpenableColumns
import android.provider.DocumentsContract
import androidx.work.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import media.qimeng.next.model.*
import java.io.File
import java.io.FileOutputStream
import java.time.Instant
import java.security.MessageDigest
import java.util.UUID
import java.util.concurrent.TimeUnit
import okhttp3.HttpUrl.Companion.toHttpUrl
import kotlin.coroutines.coroutineContext

class UploadRepository internal constructor(private val context: Context, val queue: QueueStore, private val settings: SecureSettings) {
    private val spool = File(context.filesDir, "upload-spool").apply { mkdirs() }
    private val intakeLock = Mutex()
    private val published = MutableStateFlow<Pair<Session, String>?>(null)
    val lastPublication = published.asStateFlow()

    internal fun recordPublication(task: Transfer) {
        if (sameTarget(task.target, settings.session.value)) published.value = task.target to task.id
    }

    /** Copy grants into app-owned immutable snapshots; a later provider change cannot change a retry. */
    suspend fun receive(uris: List<Uri>, intakeGroup: String? = null, archive: List<ArchiveInput>? = null): List<String> = withContext(Dispatchers.IO) {
        intakeLock.withLock {
        require(uris.size <= 100) { "每批最多选择 100 个文件" }
        val saved = queue.state.value ?: error("请先恢复传输记录")
        val initialCount = saved.pendingInputs.count { it.intakeGroup == intakeGroup }
        val selection = uris.mapIndexed { index, uri ->
            val entry = archive?.get(index)
            uri to OriginalInput(entry?.id ?: UUID.randomUUID().toString(), entry?.name, entry?.size?.takeIf { it >= 0 },
                nameState = if (entry == null) "unavailable" else "known", intakeGroup = intakeGroup)
        }
        val reserved = saved.pendingInputs.mapTo(mutableSetOf(), OriginalInput::id)
        val complete = (saved.drafts.map(UploadDraft::id) + saved.intakeFailures.map(IntakeFailure::id)).toSet()
        val additions = selection.map { it.second }.filterNot { it.id in reserved }
        queue.update { previous ->
            require(initialCount + additions.size <= 100) { "待传清单每批最多 100 项，请先处理已有选择" }
            check(previous.intakeFailures.count { it.batchKey == null } + additions.size <= 200 && previous.drafts.size + additions.size +
                previous.transfers.count { !it.state.isFinished() } <= 200) { "请先处理已有队列，再选择这一批文件" }
            previous.copy(pendingInputs = previous.pendingInputs + additions)
        }
        selection.forEachIndexed { index, (uri, original) ->
            coroutineContext.ensureActive()
            val id = original.id
            if (id in complete) return@forEachIndexed
            val unavailableName = "第${initialCount + index + 1}项（名称读取失败）"
            var name = unavailableName
            var nameKnown = false
            var declaredSize: Long? = null
            var sourceMtime: String? = null
            try {
            require(uri.scheme == "content") { "仅支持系统文件提供器的内容授权" }
            readSourceMetadata { context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null) }?.use { cursor ->
                if (cursor.moveToFirst()) {
                    val nameIndex = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                    val sizeIndex = cursor.getColumnIndex(OpenableColumns.SIZE)
                    if (nameIndex >= 0 && !cursor.isNull(nameIndex)) { name = cursor.getString(nameIndex); nameKnown = true }
                    if (sizeIndex >= 0 && !cursor.isNull(sizeIndex)) declaredSize = cursor.getLong(sizeIndex).takeIf { it >= 0 }
                }
            }
            val preview = archive?.get(index)
            check(preview == null || (name == preview.name && (preview.size == null || preview.size == declaredSize))) { "归档来源已改变，原始清单保留" }
            val validName = nameKnown && name.isNotBlank() && name.length <= 240 && '/' !in name && '\\' !in name
            queue.update { it.copy(pendingInputs = it.pendingInputs.map { input -> if (input.id == id) input.copy(
                originalFileName = name.takeIf { validName }, nameState = if (validName) "known" else "unavailable", size = declaredSize) else input }) }
            require(nameKnown) { "无法读取原始文件名，输入仍保留在清单中" }
            require(validName) { "文件名无效或过长" }
            if (DocumentsContract.isDocumentUri(context, uri)) {
                readSourceMetadata { context.contentResolver.query(uri, arrayOf(DocumentsContract.Document.COLUMN_LAST_MODIFIED), null, null, null) }?.use { cursor ->
                    if (cursor.moveToFirst() && !cursor.isNull(0)) cursor.getLong(0).takeIf { it > 0 }?.let { sourceMtime = Instant.ofEpochMilli(it).toString() }
                }
            }
            val expectedMtime = preview?.modified
            check(expectedMtime == null || expectedMtime <= 0 || sourceMtime == Instant.ofEpochMilli(expectedMtime).toString()) { "归档来源修改时间已改变，未传此文件" }
            val mime = readSourceMetadata { context.contentResolver.getType(uri) } ?: "application/octet-stream"
            val reserve = 64L * 1024 * 1024
            declaredSize?.let { require(spool.usableSpace - reserve > it) { "手机空间不足，无法保存可恢复传输副本" } }
            val file = File(spool, id)
            try {
                var count = 0L
                val digest = MessageDigest.getInstance("SHA-256")
                readSourceMetadata { context.contentResolver.openInputStream(uri) }?.use { input ->
                    FileOutputStream(file).use { output ->
                        val buffer = ByteArray(128 * 1024)
                        while (true) {
                            coroutineContext.ensureActive()
                            val read = input.read(buffer)
                            if (read < 0) break
                            check(spool.usableSpace > reserve + read) { "手机空间不足，已停止读取" }
                            output.write(buffer, 0, read)
                            digest.update(buffer, 0, read)
                            count += read
                        }
                        output.fd.sync()
                    }
                } ?: error("无法读取所选文件")
                // EOF is a known size even when the provider omitted its size. Go records empty inputs in the sealed manifest.
                require(count == 0L || declaredSize == null || declaredSize == count) { "文件长度已变化，请重新选择" }
                val draft = UploadDraft(id, name, mime, count, id, digest.digest().joinToString("") { "%02x".format(it) }, name, sourceMtime, intakeGroup)
                queue.update { previous ->
                    previous.copy(drafts = previous.drafts + draft, pendingInputs = previous.pendingInputs.map {
                        if (it.id == id) it.copy(size = count, expectedHash = draft.sourceHash) else it
                    })
                }
            } catch (failure: Exception) {
                file.delete() // Only this uncommitted app-owned snapshot, never the provider's original.
                throw failure
            }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                val safeName = name.takeIf { nameKnown && it.isNotBlank() && it.length <= 240 && '/' !in it && '\\' !in it } ?: unavailableName
                queue.update { it.copy(intakeFailures = it.intakeFailures + IntakeFailure(id, safeName,
                    if (failure is java.io.IOException || failure is SecurityException) "无法读取来源文件，请检查文件与系统授权后重试" else safeFailure(failure))) }
            }
        }
        selection.map { it.second.id }
        }
    }

    suspend fun recoverInterruptedIntake() = intakeLock.withLock {
        val snapshot = queue.state.value ?: return@withLock
        val known = (snapshot.drafts.map { it.id } + snapshot.intakeFailures.map { it.id }).toSet()
        val interrupted = snapshot.pendingInputs.filter { it.id !in known }
        val unconfirmed = snapshot.transfers.filter { it.mode == UploadMode.ORDINARY && it.state == TransferState.SUCCEEDED && it.assetId.isNullOrBlank() }.mapTo(mutableSetOf(), Transfer::id)
        val remainingCopies = withContext(Dispatchers.IO) { interrupted.filterNot { deleteUncommittedSnapshot(it.id) }.map { it.id }.toSet() }
        if (interrupted.isNotEmpty() || unconfirmed.isNotEmpty() || snapshot.pendingInputs.any { it.intakeGroup?.startsWith("share-") == true }) queue.update { it.copy(
            transfers = it.transfers.map { task -> if (task.id in unconfirmed && task.assetId.isNullOrBlank()) task.copy(
                state = if (task.uploadId != null) TransferState.QUEUED else TransferState.FAILED,
                message = if (task.uploadId != null) "正在核对原普通上传的入库回执" else "原普通上传缺少提交凭据，请核对任务报告") else task },
            pendingInputs = it.pendingInputs.map { input -> if (input.intakeGroup?.startsWith("share-") == true) input.copy(intakeGroup = null) else input },
            drafts = it.drafts.map { draft -> if (draft.intakeGroup?.startsWith("share-") == true) draft.copy(intakeGroup = null) else draft },
            intakeFailures = it.intakeFailures + interrupted.map { input ->
            IntakeFailure(input.id, input.originalFileName ?: "第${snapshot.pendingInputs.indexOf(input) + 1}项（名称读取失败）",
                "上次读取被中断，未创建可传输副本；此输入仍保留在原始清单" + if (input.id in remainingCopies) "；应用内未完成副本尚未清理" else "")
        }) }
    }

    suspend fun releaseSharedDrafts(group: String) = intakeLock.withLock {
        if (queue.state.value?.pendingInputs?.none { it.intakeGroup == group } == true) return@withLock
        queue.update { state -> state.copy(
            pendingInputs = state.pendingInputs.map { if (it.intakeGroup == group) it.copy(intakeGroup = null) else it },
            drafts = state.drafts.map { if (it.intakeGroup == group) it.copy(intakeGroup = null) else it }) }
    }

    suspend fun renameDraft(id: String, name: String) = intakeLock.withLock {
        require(name.isNotBlank() && '/' !in name && '\\' !in name && name.length <= 240) { "请输入有效文件名" }
        queue.update { it.copy(drafts = it.drafts.map { draft -> if (draft.id == id) draft.copy(name = name.trim()) else draft }) }
    }

    suspend fun discardDraft(id: String) = intakeLock.withLock {
        val draft = queue.state.value?.drafts?.find { it.id == id } ?: return@withLock
        queue.update { it.copy(drafts = it.drafts.filterNot { draft -> draft.id == id }, pendingInputs = it.pendingInputs.filterNot { input -> input.id == id }) }
        withContext(Dispatchers.IO) { File(spool, draft.spoolName).delete() }
    }

    suspend fun enqueue(libraryId: String, directory: String, mode: UploadMode, options: UploadOptions = UploadOptions(),
                        target: Session = settings.session.value ?: error("请先登录服务器"),
                        inputIds: List<String> = queue.state.value?.pendingInputs.orEmpty().filter { it.intakeGroup == null }.map { it.id },
                        reviewed: List<UploadDraft>? = null, editedStems: Map<String, String> = emptyMap(), batchKey: String = UUID.randomUUID().toString()) {
        intakeLock.withLock {
        require(libraryId.isNotBlank()) { "请选择上传影库" }
        queue.update { previous ->
            check(sameTarget(target, settings.session.value)) { "服务器已改变，请重新确认上传位置" }
            if (previous.batches.any { it.key == batchKey && sameTarget(it.target, target) }) return@update previous
            val selectedIds = inputIds.toSet()
            val inputs = previous.pendingInputs.filter { it.id in selectedIds }
            val drafts = previous.drafts.filter { it.id in selectedIds }.map { draft ->
                val stem = editedStems[draft.id]
                require(stem == null || draft.validStem(stem)) { "请输入有效的作品名" }
                draft.withEditedStem(stem)
            }
            require(inputIds.size in 1..100 && inputs.map { it.id } == inputIds) { "原始输入清单已改变，请核对本批后重试" }
            check(drafts.isNotEmpty() || (inputs.all { it.intakeGroup == batchKey } && previous.intakeFailures.any { it.id in selectedIds })) { "本批没有可读取的文件，请查看读取结果" }
            check(reviewed == null || reviewed == drafts) { "待传文件已改变，请核对本批后重试" }
            check(drafts.all { it.sourceHash.length == 64 }) { "待传快照缺少完整摘要，请重新选择文件" }
            val label = "Android ${inputs.size} 个文件"
            previous.copy(drafts = previous.drafts.filterNot { it.id in selectedIds }, pendingInputs = previous.pendingInputs.filterNot { it.id in selectedIds },
                batches = previous.batches + TransferBatch(batchKey, target, libraryId, mode, label, inputs,
                    relativeDirectory = directory.trim(), options = options),
                intakeFailures = previous.intakeFailures.map { if (it.id in selectedIds) it.copy(batchKey = batchKey) else it },
                transfers = previous.transfers + drafts.map {
                    Transfer(it.id, target, libraryId, it.name, it.mimeType, it.bytes, it.spoolName, mode, it.sourceMtime,
                        batchKey, label, directory.trim(), options = options, sourceHash = it.sourceHash, originalName = it.originalName,
                        archiveSourceState = if (batchKey.startsWith("archive-")) "pending" else null)
                })
        }
        }
        schedule()
    }

    suspend fun retry(id: String) {
        queue.update { snapshot -> snapshot.copy(transfers = snapshot.transfers.map {
            if (it.id == id && canRetry(it.state)) {
                check(sameTarget(it.target, settings.session.value)) { "请先登录此任务原来的服务器" }
                it.copy(state = TransferState.QUEUED, message = "等待核对服务器进度")
            } else it
        }) }
        schedule()
    }

    suspend fun cancel(id: String) {
        queue.update { it.copy(transfers = it.transfers.map { item ->
            if (item.id == id && !item.state.isFinished()) item.copy(state = TransferState.CANCELLED,
                message = "已停止本机发送，等待服务器确认取消；已提交的结果保留") else item
        }) }
        schedule()
    }

    suspend fun clearFinished() = intakeLock.withLock {
        val snapshot = queue.state.value ?: return@withLock
        val finished = snapshot.transfers.filter { it.state == TransferState.REJECTED ||
            (it.state == TransferState.SUCCEEDED && it.archiveSourceState in setOf(null, "done", "blocked")) ||
            (it.state == TransferState.CANCELLED && it.cancelConfirmed) }
        val removed = withContext(Dispatchers.IO) { finished.filter {
            val file = File(spool, it.spoolName)
            !file.exists() || file.delete() || !file.exists()
        }.map(Transfer::id).toSet() }
        val retained = finished.map(Transfer::id).toSet() - removed
        val sealed = snapshot.batches.filter { it.serverId != null && it.manifestFingerprint != null }.mapTo(mutableSetOf(), TransferBatch::key)
        val reserved = snapshot.pendingInputs.filter { it.intakeGroup != null }.mapTo(mutableSetOf(), OriginalInput::id)
        val clearedFailures = withContext(Dispatchers.IO) { snapshot.intakeFailures.filter {
            (it.batchKey == null && it.id !in reserved || it.batchKey in sealed) && deleteUncommittedSnapshot(it.id)
        }.map { it.id }.toSet() }
        queue.update { it.copy(transfers = it.transfers.filterNot { item -> item.id in removed }.map { item ->
            if (item.id in retained) item.copy(message = "本机传输副本清理失败；服务器结果保留，可重试清理") else item
        }, batches = it.batches.filter { batch -> it.transfers.any { task -> task.batchKey == batch.key && task.id !in removed } ||
            it.intakeFailures.any { failure -> failure.batchKey == batch.key && failure.id !in clearedFailures } },
            pendingInputs = it.pendingInputs.filterNot { input -> input.id in clearedFailures && snapshot.intakeFailures.any { failure -> failure.id == input.id && failure.batchKey == null } },
            intakeFailures = it.intakeFailures.filterNot { failure -> failure.id in clearedFailures }.map { failure ->
                if (failure.batchKey == null) failure.copy(message = "应用内未完成副本清理失败，记录已保留；来源文件未改动") else failure
            }) }
    }

    fun schedule() {
        val target = settings.session.value ?: return
        // Loopback transport (including tunnels) works with no external Android network.
        val loopback = target.baseUrl.toHttpUrl().host in setOf("127.0.0.1", "localhost", "::1")
        val targetKey = transferTargetKey(target)
        val request = OneTimeWorkRequestBuilder<TransferWorker>()
            .setInputData(Data.Builder().putString("targetKey", targetKey).build())
            .setConstraints(Constraints.Builder().setRequiredNetworkType(if (loopback) NetworkType.NOT_REQUIRED else NetworkType.CONNECTED).build())
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 20, TimeUnit.SECONDS).build()
        // A remote job waiting for Wi-Fi must not block a newly selected loopback queue.
        WorkManager.getInstance(context).enqueueUniqueWork("qimeng-transfer-$targetKey", ExistingWorkPolicy.APPEND_OR_REPLACE, request)
    }

    private fun deleteUncommittedSnapshot(id: String): Boolean {
        val file = File(spool, id)
        check(file.parentFile?.canonicalFile == spool.canonicalFile) { "传输副本位置无效，已停止清理" }
        return !file.exists() || file.delete() || !file.exists()
    }
}

internal fun transferTargetKey(target: Session) = MessageDigest.getInstance("SHA-256")
    .digest((target.baseUrl + "\u0000" + target.instanceId).toByteArray(Charsets.UTF_8)).joinToString("") { "%02x".format(it) }

private inline fun <T> readSourceMetadata(read: () -> T): T = try { read() }
catch (failure: CancellationException) { throw failure }
catch (_: Exception) { throw java.io.IOException("文件提供器读取失败") }
