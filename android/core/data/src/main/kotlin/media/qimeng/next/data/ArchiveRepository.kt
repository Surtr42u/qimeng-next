package media.qimeng.next.data

import android.content.Context
import android.content.Intent
import android.database.Cursor
import android.net.Uri
import android.provider.DocumentsContract
import android.provider.DocumentsContract.Document
import android.webkit.MimeTypeMap
import androidx.work.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import media.qimeng.next.model.*
import media.qimeng.next.sdk.model.InputDirectoryNames
import java.util.UUID
import java.util.concurrent.TimeUnit

class ArchiveRepository internal constructor(private val context: Context, private val provider: ApiProvider, private val settings: SecureSettings) {
    internal val store = ArchiveStore(context)
    private val prefs = context.getSharedPreferences("archive-source", Context.MODE_PRIVATE)
    private val currentTree = MutableStateFlow(prefs.getString("tree", null))
    val tree = currentTree.asStateFlow()
    private val changes = MutableStateFlow(0L)
    val revision = changes.asStateFlow()
    private val scanner = Mutex()

    suspend fun select(uri: Uri?, flags: Int = 0) = withContext(Dispatchers.IO) {
        val previous = currentTree.value
        if (uri != null) {
            require(uri.scheme == "content" && DocumentsContract.isTreeUri(uri)) { "请选择系统提供的归档文件夹" }
            val persisted = flags and (Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
            check(persisted and Intent.FLAG_GRANT_READ_URI_PERMISSION != 0) { "系统未授予读取权限" }
            context.contentResolver.takePersistableUriPermission(uri, persisted)
        }
        check(prefs.edit().putString("tree", uri?.toString()).commit()) { "归档文件夹设置未能保存" }
        currentTree.value = uri?.toString()
        if (previous != null && previous != currentTree.value && !store.needsGrant(previous)) {
            val old = Uri.parse(previous)
            context.contentResolver.persistedUriPermissions.find { it.uri == old }?.let { grant ->
                val access = (if (grant.isReadPermission) Intent.FLAG_GRANT_READ_URI_PERMISSION else 0) or
                    (if (grant.isWritePermission) Intent.FLAG_GRANT_WRITE_URI_PERMISSION else 0)
                if (access != 0) context.contentResolver.releasePersistableUriPermission(old, access)
            }
        }
    }

    suspend fun latest(target: Session): ArchiveOverview? = withContext(Dispatchers.IO) { store.latest(target)?.let(store::overview) }

    suspend fun scan(target: Session, local: Boolean): ArchiveOverview = withContext(Dispatchers.IO) {
        scanner.withLock {
            val tree = Uri.parse(currentTree.value ?: error("请先设置归档文件夹"))
            val previous = store.latest(target)?.let(store::overview)
            check(previous?.state !in setOf("confirmed", "preparing")) { "上一批归档仍在入队" }
            val plan = store.start(target, tree.toString(), local)
            try {
                val root = DocumentsContract.buildDocumentUriUsingTree(tree, DocumentsContract.getTreeDocumentId(tree))
                val candidates = ArrayList<Pair<String, String>>(100)
                suspend fun route() {
                    if (candidates.isEmpty()) return
                    val names = candidates.map { it.first }.distinct()
                    val decisions = provider.api(target).resolveInputDirectories(InputDirectoryNames(names)).bodyOrThrow().items.associateBy { it.name }
                    candidates.forEach { (name, uri) ->
                        val match = decisions[name] ?: error("服务器未返回归档子目录的去向")
                        if (match.state.value == "matched") store.folder(plan, uri, requireNotNull(match.libraryId), "")
                        else store.exclude(plan, name, match.state.value)
                    }
                    candidates.clear()
                }
                children(tree, root) { child ->
                    if (child.type == Document.MIME_TYPE_DIR) {
                        candidates.add(child.name to child.uri.toString())
                        if (candidates.size == 100) route()
                    }
                }
                route()
                while (true) {
                    currentCoroutineContext().ensureActive()
                    val folder = store.nextFolder(plan) ?: break
                    val files = ArrayList<ArchiveInput>(100)
                    val trash = ArchiveMediaTrash(context)
                    children(tree, Uri.parse(folder.uri)) { child ->
                        if (child.type == Document.MIME_TYPE_DIR) {
                            val relative = if (folder.directory.isEmpty()) child.name else "${folder.directory}/${child.name}"
                            store.folder(plan, child.uri.toString(), folder.library, relative)
                        } else if (child.media) {
                            val binding = if (local) null else try { trash.identify(child.uri) }
                                catch (_: SecurityException) { null } catch (_: java.io.IOException) { null } catch (_: android.system.ErrnoException) { null }
                            files.add(ArchiveInput(UUID.randomUUID().toString(), child.uri.toString(), child.name, child.size, child.modified,
                                binding?.uri?.toString(), binding?.generation, binding?.fileIdentity))
                            if (files.size == 100) { store.inputs(plan, folder, files); files.clear() }
                        }
                    }
                    if (files.isNotEmpty()) store.inputs(plan, folder, files)
                    store.scannedFolder(plan, folder.uri)
                }
                check(sameTarget(target, settings.session.value) && tree.toString() == currentTree.value) { "归档位置或服务器已改变，请重新扫描" }
                val manifest = store.sealPreview(plan)
                if (previous != null && previous.state == "queued" && previous.tree == tree.toString() && store.sameManifest(previous.id, manifest)) {
                    store.discardPreview(plan)
                    return@withLock previous
                }
                previous?.let { store.discardPreview(it.id) }
                requireNotNull(store.overview(plan))
            } catch (failure: Throwable) {
                store.discardPreview(plan)
                throw failure
            }
        }
    }

    suspend fun confirm(plan: ArchiveOverview) = withContext(Dispatchers.IO) {
        check(sameTarget(plan.target, settings.session.value) && plan.tree == currentTree.value) { "归档位置或目标服务器已改变" }
        val now = store.overview(plan.id) ?: error("归档清单已更换，请重新核对")
        check(now.files > 0 && now.state in setOf("preview", "confirmed", "preparing")) { "当前归档清单不可提交" }
        if (now.state == "preview") store.state(plan.id, "confirmed")
        schedule(plan.id)
        changed()
    }

    internal fun schedule(id: String) {
        val request = OneTimeWorkRequestBuilder<ArchiveIntakeWorker>().setInputData(workDataOf("plan" to id))
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 15, TimeUnit.SECONDS).build()
        WorkManager.getInstance(context).enqueueUniqueWork("archive-intake-$id", ExistingWorkPolicy.KEEP, request)
    }
    internal fun changed() = changes.update { it + 1 }
    suspend fun prune(ids: Set<String>) = withContext(Dispatchers.IO) { if (ids.isNotEmpty()) store.prune(ids) }
    suspend fun resume(target: Session) = withContext(Dispatchers.IO) {
        latest(target)?.takeIf { it.state in setOf("confirmed", "preparing") }?.let { schedule(it.id) }
    }
    suspend fun retryProtection(task: Transfer) = withContext(Dispatchers.IO) {
        check(sameTarget(task.target, settings.session.value)) { "请先连接原来的服务器" }
        val source = store.cleanup(task.id) ?: error("来源核对记录已清理")
        if (source.stage == "pending") return@withContext
        check(source.stage == "blocked") { "此来源不在等待重试状态" }
        val batch = requireNotNull(task.batchId)
        val api = provider.api(task.target)
        source.protection?.let { id ->
            val existing = api.getBatchProtection(batch, id).bodyOrThrow()
            if (existing.state.value in setOf("failed", "interrupted")) {
                api.retryBatchProtection(batch, id, UUID.randomUUID().toString()).bodyOrThrow()
            } else if (existing.state.value in setOf("completed", "historical")) {
                val proof = api.protectBatchInputs(batch, "recheck-${task.id}-$id").bodyOrThrow()
                store.protection(task.batchKey, proof.id)
            }
        }
        store.retry(task.id)
    }

    private data class Child(val uri: Uri, val name: String, val type: String?, val size: Long?, val modified: Long?) {
        val media: Boolean get() {
            val mime = type?.takeUnless { it == "application/octet-stream" } ?: MimeTypeMap.getSingleton().getMimeTypeFromExtension(name.substringAfterLast('.', "").lowercase())
            return mime?.let { it.startsWith("image/") || it.startsWith("video/") } == true
        }
    }
    private suspend fun children(tree: Uri, parent: Uri, visit: suspend (Child) -> Unit) {
        val uri = DocumentsContract.buildChildDocumentsUriUsingTree(tree, DocumentsContract.getDocumentId(parent))
        val columns = arrayOf(Document.COLUMN_DOCUMENT_ID, Document.COLUMN_DISPLAY_NAME, Document.COLUMN_MIME_TYPE, Document.COLUMN_SIZE, Document.COLUMN_LAST_MODIFIED)
        val cursor = try { context.contentResolver.query(uri, columns, null, null, null) }
        catch (_: SecurityException) { throw IllegalStateException("归档目录权限已撤回，请重新选择文件夹") }
        cursor?.use { rows ->
            check(!rows.extras.getBoolean(DocumentsContract.EXTRA_LOADING)) { "文件提供器正在加载，请稍后重新扫描" }
            while (rows.moveToNext()) {
                currentCoroutineContext().ensureActive()
                val name = rows.getString(1) ?: error("归档目录含无法读取名称的条目")
                require(name.isNotBlank() && name !in setOf(".", "..") && '/' !in name && '\\' !in name && '\u0000' !in name) { "文件提供器返回了无效的目录项名称" }
                visit(Child(DocumentsContract.buildDocumentUriUsingTree(tree, rows.getString(0)), name, rows.getString(2),
                    rows.optionalLong(3), rows.optionalLong(4)))
            }
        } ?: error("归档目录未能读取")
    }
    private fun Cursor.optionalLong(column: Int): Long? = if (isNull(column)) null else getLong(column).takeIf { it >= 0 }
}
