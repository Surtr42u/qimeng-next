package media.qimeng.next.data

import android.content.ContentResolver
import android.net.Uri
import android.provider.DocumentsContract
import android.provider.DocumentsContract.Document
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import org.json.JSONObject
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.security.MessageDigest

internal data class BackupGeneration(val instanceId: String, val businessRevision: String, val backupId: String, val hashes: Map<String, String>) {
    fun encode(): String = JSONObject().put("instanceId", instanceId).put("businessRevision", businessRevision)
        .put("backupId", backupId).put("hashes", JSONObject(hashes)).toString()

    companion object {
        fun decode(text: String): BackupGeneration {
            val json = JSONObject(text)
            val id = json.getString("backupId")
            val revision = json.getString("businessRevision")
            check(id.matches(Regex("[a-zA-Z0-9_-]{1,200}")) && revision.matches(Regex("[0-9]+"))) { "维护目录代次标识无效" }
            val hashes = maintenanceParts.keys.associateWith { key -> json.getJSONObject("hashes").getString(key).also {
                check(it.matches(Regex("[a-f0-9]{64}"))) { "维护目录的完整性摘要无效" }
            } }
            return BackupGeneration(json.getString("instanceId"), revision, id, hashes)
        }
    }
}

internal val maintenanceParts = linkedMapOf("archive" to ("备份" to "影库备份.qnext"), "master" to ("作者资料" to "作者总表.txt"),
    "report" to ("作者资料" to "作者资料.md"), "index" to ("作者资料" to "作者片段索引.json"))
private const val generationRecord = ".绮梦下一代清单.json"
private const val reusableFolder = ".绮梦手机轮换空目录"
internal const val mobileRotationMarker = ".绮梦手机轮换.json"
internal const val webRotationMarker = ".绮梦下一代轮换.json"
internal const val maintenanceReadmeName = "目录说明.md"

/** 维护目录说明（与旧版同名同位置）；只描述用户可见的固定结构，不写本机路径。 */
internal val maintenanceReadme = """# 影库资料维护

这里统一保存作者资料、记录备份和上传归档。换设备时，先同步媒体，再用配套离线命令恢复记录备份。

## 文件夹一览

| 文件夹 | 你会用到的内容 | 日常操作 |
| --- | --- | --- |
| 当前/作者资料 | 作者总表.txt、作者资料.md、作者片段索引.json | TXT 维护作者与作品关联，Markdown 阅读简介、来源和作品清单 |
| 当前/备份 | 影库备份.qnext 和同名 .sha256 | 记录快照（不含作品原件），使用配套离线命令核验与恢复 |
| 上次 | 上一代作者资料和记录备份 | 需要回退时按需复制，程序不会自动清理 |
| 上传归档 | 按媒体库和目标目录保存的原件副本 | 查看已经上传成功的文件 |

## 当前与上次两代

- 内容变化时先把整个「当前」提交为「上次」，再写新的「当前」；内容没变时只刷新当前，不挤掉上次。
- 每份备份、TXT 和索引都有同名 .sha256，复制时一起带走，不需要手动编辑。
- 摘要不符、文件不可读或目录被外部改动时程序会停止覆盖并保留现场。
- .qnext 记录备份包含业务资料与作者头像，不含作品原件；恢复后需把原件按原库路径放回，再按服务端版本配套的离线命令使用。
- 以「.绮梦」开头的文件和暂存目录是程序内部状态，请勿编辑或删除；轮换结束后它们会自动清理。
"""


/** The granted provider owns all paths. Fixed child names and verified bytes are the only publication inputs. */
internal class MaintenanceTree(private val resolver: ContentResolver, val tree: Uri) {
    val root: Uri = DocumentsContract.buildDocumentUriUsingTree(tree, DocumentsContract.getTreeDocumentId(tree))
    data class Entry(val uri: Uri, val name: String, val directory: Boolean)

    private fun children(parent: Uri, accept: (Entry) -> Unit) {
        val query = DocumentsContract.buildChildDocumentsUriUsingTree(tree, DocumentsContract.getDocumentId(parent))
        val columns = arrayOf(Document.COLUMN_DOCUMENT_ID, Document.COLUMN_DISPLAY_NAME, Document.COLUMN_MIME_TYPE)
        val cursor = resolver.query(query, columns, null, null, null) ?: error("系统未能读取维护目录")
        cursor.use {
            check(!it.extras.getBoolean(DocumentsContract.EXTRA_LOADING)) { "系统正在加载维护目录" }
            while (it.moveToNext()) accept(Entry(DocumentsContract.buildDocumentUriUsingTree(tree, it.getString(0)),
                it.getString(1), it.getString(2) == Document.MIME_TYPE_DIR))
        }
    }
    fun find(parent: Uri, name: String): Entry? {
        var found: Entry? = null
        children(parent) { if (it.name == name) { check(found == null) { "维护目录中存在同名条目" }; found = it } }
        return found
    }
    fun directory(parent: Uri, name: String): Uri = find(parent, name)?.let {
        check(it.directory) { "维护目录名被同名文件占用" }; it.uri
    } ?: create(parent, name, Document.MIME_TYPE_DIR)
    fun staging(name: String): Uri {
        if (find(root, name) == null) find(root, reusableFolder)?.let {
            if (it.directory && onlyEmptyManagedFolders(it.uri)) rename(reusableFolder,name)
        }
        return directory(root,name)
    }
    private fun create(parent: Uri, name: String, mime: String): Uri {
        check(name.isNotBlank() && name !in setOf(".", "..") && '/' !in name && '\\' !in name) { "维护文件名无效" }
        val created = DocumentsContract.createDocument(resolver, parent, mime, name) ?: error("文件提供器未建立维护文件")
        val entry = find(parent, name)
        check(entry?.uri == created) { "文件提供器未保留所需文件名，原有内容保留" }
        return created
    }
    fun open(uri: Uri): InputStream = resolver.openInputStream(uri) ?: error("系统未能打开维护文件")
    fun smallText(uri: Uri, limit: Int = 16_384): String = open(uri).use { input ->
        val bytes = ByteArrayOutputStream()
        val buffer = ByteArray(4096)
        while (true) {
            val count = input.read(buffer)
            if (count < 0) break
            check(bytes.size() + count <= limit) { "维护目录中的清单超过长度限制" }
            bytes.write(buffer, 0, count)
        }
        bytes.toString("UTF-8")
    }

    /** Existing bytes in a unique staging file can only be extended after a byte-for-byte prefix check. */
    suspend fun write(parent: Uri, name: String, input: InputStream, knownHash: String? = null): String {
        val existing = find(parent, name)
        check(existing?.directory != true) { "维护文件名已被文件夹占用" }
        val mime = android.webkit.MimeTypeMap.getSingleton().getMimeTypeFromExtension(name.substringAfterLast('.').lowercase()) ?: "application/octet-stream"
        val file = existing?.uri ?: create(parent, name, mime)
        val digest = MessageDigest.getInstance("SHA-256")
        val incoming = ByteArray(128 * 1024)
        var prefix = 0L
        if (existing != null) {
            val current = ByteArray(incoming.size)
            open(file).use { previous ->
                while (true) {
                    currentCoroutineContext().ensureActive()
                    val count = previous.read(current)
                    if (count < 0) break
                    var n = 0
                    while (n < count) {
                        val read = input.read(incoming, n, count - n)
                        check(read > 0) { "维护暂存文件比源资料长，已保留原内容" }
                        n += read
                    }
                    for (i in 0 until count) check(current[i] == incoming[i]) { "维护暂存文件已被外部修改，已停止写入" }
                    digest.update(current, 0, count); prefix += count
                }
            }
        }
        var count = input.read(incoming)
        if (count >= 0) {
            val output = resolver.openOutputStream(file, if (prefix == 0L) "wt" else "wa") ?: error("维护目录不能写入")
            output.use {
                while (count >= 0) {
                    currentCoroutineContext().ensureActive()
                    it.write(incoming, 0, count); digest.update(incoming, 0, count)
                    count = input.read(incoming)
                }
            }
        }
        val hash = digest.digest().hex()
        check((knownHash == null || hash == knownHash) && this.hash(file) == hash) { "维护文件写后核验失败，原两代资料保留" }
        return hash
    }
    suspend fun writeText(parent: Uri, name: String, text: String): String =
        ByteArrayInputStream(text.toByteArray(Charsets.UTF_8)).use { write(parent, name, it) }

    /** 覆盖写短文本（目录说明等非断点续传文件）；写后按字节摘要核验。 */
    suspend fun overwriteText(parent: Uri, name: String, text: String): String {
        val bytes = text.toByteArray(Charsets.UTF_8)
        val expected = MessageDigest.getInstance("SHA-256").digest(bytes).hex()
        val existing = find(parent, name)
        check(existing?.directory != true) { "维护文件名已被文件夹占用" }
        val mime = android.webkit.MimeTypeMap.getSingleton().getMimeTypeFromExtension(name.substringAfterLast('.').lowercase())
            ?: "application/octet-stream"
        val file = existing?.uri ?: create(parent, name, mime)
        resolver.openOutputStream(file, "wt")?.use { it.write(bytes) } ?: error("维护目录不能写入")
        check(hash(file) == expected) { "维护文件写后核验失败，原内容保留" }
        return expected
    }
    suspend fun hash(uri: Uri): String {
        val digest = MessageDigest.getInstance("SHA-256")
        open(uri).use { stream ->
            val buffer = ByteArray(128 * 1024)
            while (true) {
                currentCoroutineContext().ensureActive()
                val count = stream.read(buffer)
                if (count < 0) break
                digest.update(buffer, 0, count)
            }
        }
        return digest.digest().hex()
    }
    fun generation(name: String): BackupGeneration? {
        val folder = find(root, name) ?: return null
        check(folder.directory) { "维护代次被同名文件占用" }
        val record = find(folder.uri, generationRecord) ?: error("已有代次缺少清单，已保留原目录")
        return BackupGeneration.decode(smallText(record.uri))
    }
    suspend fun part(folder: Uri, key: String, input: InputStream, hash: String? = null): String {
        val (subdir, name) = maintenanceParts.getValue(key)
        val parent = directory(folder, subdir)
        val actual = write(parent, name, input, hash)
        writeText(parent, "$name.sha256", "$actual\n")
        return actual
    }
    suspend fun seal(folder: Uri, generation: BackupGeneration) { writeText(folder, generationRecord, generation.encode()) }
    suspend fun discardMasterStage(slot: String, expectedHash: String) {
        val folder = find(root, slot)?.uri ?: return
        val sub = find(folder, "作者资料")?.uri ?: return
        find(sub, "作者总表.txt")?.let { check(hash(it.uri) == expectedHash) { "暂存作者资料被修改，已保留" }; remove(it.uri) }
        find(sub, "作者总表.txt.sha256")?.let { check(smallText(it.uri, 256).trim() == expectedHash); remove(it.uri) }
        releaseFolder(slot)
    }
    suspend fun verify(slot: String, expected: BackupGeneration) {
        check(generation(slot) == expected) { "维护目录的代次已改变" }
        val folder = requireNotNull(find(root, slot)).uri
        for ((key, path) in maintenanceParts) {
            val parent = find(folder, path.first)?.uri ?: error("代次中缺少资料目录")
            val file = find(parent, path.second)?.uri ?: error("代次中缺少资料文件")
            val side = find(parent, "${path.second}.sha256")?.uri ?: error("代次中缺少独立摘要")
            val expectedHash = expected.hashes.getValue(key)
            check(smallText(side, 256).trim() == expectedHash && hash(file) == expectedHash) { "维护目录中的文件与登记摘要不一致，原文件保留" }
        }
    }
    fun rename(from: String, to: String) {
        val source = find(root, from) ?: error("待轮换的目录缺失")
        check(source.directory && find(root, to) == null) { "轮换目标已存在，已停止写回" }
        val changed = DocumentsContract.renameDocument(resolver, source.uri, to) ?: error("文件提供器不支持目录改名")
        check(find(root, to)?.uri == changed && find(root, from) == null) { "文件提供器未能确认目录改名" }
    }
    suspend fun removeExpired(slot: String, expected: BackupGeneration) {
        val directory = find(root, slot)?.uri ?: return
        // Each retired system-created file is checked individually. User additions are left in place.
        val record = find(directory, generationRecord) ?: run { releaseFolder(slot); return }
        check(BackupGeneration.decode(smallText(record.uri)) == expected) { "旧代清单已变化，已保留" }
        for ((key, path) in maintenanceParts) {
            val parent = find(directory, path.first)?.uri ?: continue
            val hash = expected.hashes.getValue(key)
            find(parent, path.second)?.let { check(this.hash(it.uri) == hash) { "旧代内容有外部修改，已保留" }; remove(it.uri) }
            find(parent, "${path.second}.sha256")?.let { check(smallText(it.uri, 256).trim() == hash) { "旧代摘要已变化" }; remove(it.uri) }
        }
        remove(record.uri)
        releaseFolder(slot)
    }
    fun remove(uri: Uri) {
        val mime = resolver.getType(uri)
        check(mime != null && mime != Document.MIME_TYPE_DIR) { "无法确认待清理的是普通维护文件" }
        check(DocumentsContract.deleteDocument(resolver, uri)) { "系统未确认维护文件清理" }
    }

    /**
     * 放弃一次未完成的轮换：只清理本次 token 命名的暂存目录中的程序文件。
     * 不触碰「当前 / 上次 / 上传归档」及用户自行放入的其他内容。
     */
    suspend fun discardStage(token: String) {
        val name = ".绮梦手机暂存-$token"
        val folder = find(root, name) ?: return
        if (!folder.directory) return
        for ((_, path) in maintenanceParts) {
            val parent = find(folder.uri, path.first) ?: continue
            if (!parent.directory) continue
            find(parent.uri, path.second)?.let { if (!it.directory) remove(it.uri) }
            find(parent.uri, "${path.second}.sha256")?.let { if (!it.directory) remove(it.uri) }
        }
        dropEmptyFolder(name)
    }

    /** 只删除程序自己建立且已空的暂存/空目录；有用户内容时原样保留。 */
    fun dropEmptyFolder(name: String) {
        val folder = find(root, name) ?: return
        if (!folder.directory || !onlyEmptyManagedFolders(folder.uri)) return
        children(folder.uri) { child -> if (child.directory) removeEmptyDirectory(child.uri) }
        removeEmptyDirectory(folder.uri)
    }

    fun dropReusableFolder() = dropEmptyFolder(reusableFolder)

    private fun removeEmptyDirectory(directory: Uri) {
        var empty = true
        children(directory) { empty = false }
        if (empty) runCatching { DocumentsContract.deleteDocument(resolver, directory) }
    }
    // A DocumentsProvider may recursively delete a directory even if another app added a child after listing it.
    // Reuse the empty staging folder on the next rotation. Never request deletion of a directory.
    private fun releaseFolder(name: String) {
        val folder = find(root,name) ?: return
        if (folder.directory && find(root,reusableFolder) == null && onlyEmptyManagedFolders(folder.uri)) rename(name,reusableFolder)
    }
    private fun onlyEmptyManagedFolders(dir: Uri): Boolean {
        var valid = true
        children(dir) { child ->
            if (!child.directory || child.name !in setOf("备份","作者资料")) valid = false
            else children(child.uri) { valid = false }
        }
        return valid
    }
}
private fun ByteArray.hex() = joinToString("") { "%02x".format(it) }
