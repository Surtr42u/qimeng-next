package media.qimeng.next.data

import android.content.Context
import android.net.Uri
import android.provider.OpenableColumns
import androidx.work.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.withContext
import media.qimeng.next.model.*
import java.io.File
import java.io.FileOutputStream
import java.time.Instant
import java.util.UUID
import java.util.concurrent.TimeUnit
import kotlin.coroutines.coroutineContext

class UploadRepository internal constructor(private val context: Context, val queue: QueueStore, private val settings: SecureSettings) {
    private val spool = File(context.filesDir, "upload-spool").apply { mkdirs() }

    /** Copy grants into app-owned immutable snapshots; a later provider change cannot change a retry. */
    suspend fun receive(uris: List<Uri>) = withContext(Dispatchers.IO) {
        require(uris.size <= 100) { "每批最多选择 100 个文件" }
        check(queue.state.value != null) { "请先恢复传输记录" }
        uris.forEach { uri ->
            coroutineContext.ensureActive()
            require(uri.scheme == "content") { "仅支持系统文件提供器的内容授权" }
            val id = UUID.randomUUID().toString()
            var name = "媒体-$id"
            var declaredSize: Long? = null
            context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { cursor ->
                if (cursor.moveToFirst()) {
                    val nameIndex = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                    val sizeIndex = cursor.getColumnIndex(OpenableColumns.SIZE)
                    if (nameIndex >= 0 && !cursor.isNull(nameIndex)) name = cursor.getString(nameIndex)
                    if (sizeIndex >= 0 && !cursor.isNull(sizeIndex)) declaredSize = cursor.getLong(sizeIndex).takeIf { it >= 0 }
                }
            }
            require(name.isNotBlank() && '/' !in name && '\\' !in name) { "文件名无效" }
            val mime = context.contentResolver.getType(uri) ?: error("文件提供器没有声明媒体类型")
            require(mime.startsWith("image/") || mime.startsWith("video/")) { "暂不支持此文件类型" }
            val reserve = 64L * 1024 * 1024
            declaredSize?.let { require(spool.usableSpace - reserve > it) { "手机空间不足，无法保存可恢复传输副本" } }
            val file = File(spool, id)
            try {
                var count = 0L
                context.contentResolver.openInputStream(uri)?.use { input ->
                    FileOutputStream(file).use { output ->
                        val buffer = ByteArray(128 * 1024)
                        while (true) {
                            coroutineContext.ensureActive()
                            val read = input.read(buffer)
                            if (read < 0) break
                            check(spool.usableSpace > reserve + read) { "手机空间不足，已停止读取" }
                            output.write(buffer, 0, read)
                            count += read
                        }
                        output.fd.sync()
                    }
                } ?: error("无法读取所选文件")
                require(count > 0 && (declaredSize == null || declaredSize == count)) { "文件长度已变化，请重新选择" }
                val draft = UploadDraft(id, name, mime, count, id)
                queue.update { previous ->
                    check(previous.drafts.size + previous.transfers.count { it.state !in setOf(TransferState.SUCCEEDED, TransferState.CANCELLED) } < 200) { "待传队列已满" }
                    previous.copy(drafts = previous.drafts + draft)
                }
            } catch (failure: Exception) {
                file.delete() // Only this uncommitted app-owned snapshot, never the provider's original.
                throw failure
            }
        }
    }

    suspend fun renameDraft(id: String, name: String) {
        require(name.isNotBlank() && '/' !in name && '\\' !in name && name.length <= 240) { "请输入有效文件名" }
        queue.update { it.copy(drafts = it.drafts.map { draft -> if (draft.id == id) draft.copy(name = name.trim()) else draft }) }
    }

    suspend fun discardDraft(id: String) {
        val draft = queue.state.value?.drafts?.find { it.id == id } ?: return
        queue.update { it.copy(drafts = it.drafts.filterNot { draft -> draft.id == id }) }
        withContext(Dispatchers.IO) { File(spool, draft.spoolName).delete() }
    }

    suspend fun enqueue(libraryId: String, directory: String, mode: UploadMode) {
        val target = settings.session.value ?: error("请先登录服务器")
        require(libraryId.isNotBlank()) { "请选择上传影库" }
        val now = Instant.now().toString()
        val batchKey = UUID.randomUUID().toString()
        queue.update { previous ->
            check(previous.drafts.isNotEmpty()) { "请先选择文件" }
            require(previous.drafts.size <= 100) { "每批最多 100 个文件，请先移除多余的待传项" }
            previous.copy(drafts = emptyList(), transfers = previous.transfers + previous.drafts.map {
                Transfer(it.id, target, libraryId, it.name, it.mimeType, it.bytes, it.spoolName, mode, now,
                    batchKey, "Android ${previous.drafts.size} 个文件", directory.trim())
            })
        }
        schedule()
    }

    suspend fun event(asset: Asset, kind: String, positionMs: Long = 0) {
        val target = settings.session.value ?: return
        queue.update { snapshot ->
            check(snapshot.events.size < 1000) { "浏览记录补传队列已满，请连接服务器后再试" }
            snapshot.copy(events = snapshot.events + PendingEvent(UUID.randomUUID().toString(), target, asset.id, kind, positionMs.coerceAtLeast(0), asset.version))
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
            if (item.id == id && item.state != TransferState.SUCCEEDED) item.copy(state = TransferState.CANCELLED,
                message = "已停止后续发送；已在服务器提交的结果保留") else item
        }) }
    }

    suspend fun clearFinished() {
        val snapshot = queue.state.value ?: return
        val finished = snapshot.transfers.filter { it.state in setOf(TransferState.SUCCEEDED, TransferState.CANCELLED) }
        queue.update { it.copy(transfers = it.transfers.filterNot { item -> item.id in finished.map(Transfer::id) }) }
        withContext(Dispatchers.IO) { finished.forEach { File(spool, it.spoolName).delete() } }
    }

    fun schedule() {
        val request = OneTimeWorkRequestBuilder<TransferWorker>()
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 20, TimeUnit.SECONDS).build()
        WorkManager.getInstance(context).enqueueUniqueWork("qimeng-transfer", ExistingWorkPolicy.APPEND_OR_REPLACE, request)
    }
}
