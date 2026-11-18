package media.qimeng.next.data

import android.content.Context
import android.net.Uri
import android.provider.OpenableColumns
import com.google.gson.JsonParser
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.withContext
import media.qimeng.next.model.*
import media.qimeng.next.sdk.model.AuthorListMirrorUpdate
import media.qimeng.next.sdk.model.AuthorListResult
import media.qimeng.next.sdk.model.ImportAuthorListRequest
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.nio.ByteBuffer
import java.util.UUID

class AuthorEntriesConflict(val missing: MissingAuthorWorks) : IOException("新片段缺少手动挂靠的作品，请核对后决定。")

class AuthorListRepository internal constructor(private val context: Context, private val provider: ApiProvider) {
    suspend fun page(target: Session, cursor: String? = null): Page<AuthorFragment> {
        val data = provider.api(target).listAuthorLists(cursor, 50).bodyOrThrow()
        return Page(data.items.map { AuthorFragment(it.id, it.name, it.revision, it.authorCount, it.workCount) }, data.nextCursor)
    }

    suspend fun prepare(target: Session, uri: Uri): AuthorImportDraft = withContext(Dispatchers.IO) {
        require(uri.scheme == "content") { "请选择系统文件提供器中的作者 TXT" }
        val name = context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use {
            if (it.moveToFirst()) it.getString(0) else null
        } ?: error("无法读取作者 TXT 文件名")
        val content = context.contentResolver.openInputStream(uri)?.use { readAuthorText(it) } ?: error("无法打开作者 TXT")
        prepare(target, name, content)
    }

    internal suspend fun prepare(target: Session, name: String, content: String): AuthorImportDraft {
        require(name.endsWith(".txt", ignoreCase = true) && name.length <= 240 && '/' !in name && '\\' !in name) { "请选择有效的 .txt 文件" }
        var cursor: String? = null
        var revision = 0L
        do {
            val chunk = page(target, cursor)
            val existing = chunk.items.find { it.name == name }
            if (existing != null) { revision = existing.revision; break }
            cursor = chunk.nextCursor
        } while (cursor != null)
        return AuthorImportDraft(target, name, content, revision, UUID.randomUUID().toString())
    }

    suspend fun applyDraft(draft: AuthorImportDraft, resolution: String? = null, key: String = draft.key): AuthorImportSummary {
        val input = ImportAuthorListRequest(draft.name, draft.content, draft.revision,
            resolution?.let { choice -> ImportAuthorListRequest.ConflictResolution.entries.first { it.value == choice } })
        val result = provider.api(draft.target).importAuthorList(key, input)
        if (result.code() == 409) {
            val body = result.errorBody()?.use { response ->
                val buffer = okio.Buffer()
                val source = response.source()
                while (buffer.size <= 256 * 1024) {
                    if (source.read(buffer, minOf(8192, 256 * 1024 + 1 - buffer.size)) < 0) break
                }
                if (buffer.size > 256 * 1024) null else buffer.readUtf8()
            }
            val error = try { body?.let { JsonParser.parseString(it).asJsonObject } } catch (_: RuntimeException) { null }
            if (error?.get("code")?.asString == "author_list_missing_entries") {
                val details = error.getAsJsonObject("details") ?: throw IOException("服务器未返回可核对的作者冲突")
                val examples = details.getAsJsonArray("missing").take(100).map { item ->
                    item.asJsonObject.get("authorName").asString to item.asJsonObject.get("fileName").asString
                }
                throw AuthorEntriesConflict(MissingAuthorWorks(details.get("totalMissing").asInt, examples))
            }
            throw ApiFailure(409, "作者清单已改变，请重新选择文件并核对。")
        }
        return result.bodyOrThrow().summary()
    }
    suspend fun remove(target: Session, list: AuthorFragment, key: String) {
        val response = provider.api(target).deleteAuthorList(list.id, list.revision, key)
        if (!response.isSuccessful) response.bodyOrThrow()
    }
    suspend fun rebuild(target: Session, key: String) = provider.api(target).rebuildAuthorListMatches(key).bodyOrThrow().summary()
    suspend fun mirror(target: Session): AuthorMirror = provider.api(target).getAuthorListMirror().bodyOrThrow().let {
        AuthorMirror(it.revision, it.path, it.mode.value, it.listId, it.state.value, it.errorCode)
    }
    suspend fun saveMirror(target: Session, initial: AuthorMirror, path: String, fragment: Boolean, listId: String?, key: String): AuthorMirror {
        val mode = AuthorListMirrorUpdate.Mode.entries.first { it.value == if (fragment) "fragment" else "master" }
        provider.api(target).updateAuthorListMirror(key, AuthorListMirrorUpdate(initial.revision, path.trim(), mode, listId?.takeIf { fragment && it.isNotBlank() })).bodyOrThrow()
        return mirror(target)
    }
    private fun AuthorListResult.summary() = AuthorImportSummary(authorsCreated, matchedAssets, changedAssets, issues.map { "第 ${it.line} 行 · ${it.code}" })
}

internal suspend fun readAuthorText(input: java.io.InputStream): String {
    val bytes = ByteArrayOutputStream()
    val buffer = ByteArray(8192)
    while (true) {
        currentCoroutineContext().ensureActive()
        val count = input.read(buffer)
        if (count < 0) break
        require(bytes.size() + count <= 1024 * 1024) { "作者 TXT 超过导入上限 1 MiB" }
        bytes.write(buffer, 0, count)
    }
    return try { Charsets.UTF_8.newDecoder().decode(ByteBuffer.wrap(bytes.toByteArray())).toString().removePrefix("\uFEFF") }
    catch (_: java.nio.charset.CharacterCodingException) { throw IOException("作者 TXT 需要 UTF-8 编码") }
}
