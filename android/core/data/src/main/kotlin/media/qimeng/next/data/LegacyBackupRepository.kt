package media.qimeng.next.data

import android.content.Context
import android.net.Uri
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.withContext
import media.qimeng.next.model.*
import media.qimeng.next.sdk.model.CommitV2MigrationRequest
import media.qimeng.next.sdk.model.V2MigrationPlan
import media.qimeng.next.sdk.model.V2PlanCreate
import media.qimeng.next.sdk.model.V2PlanCreateAuthorMappingsInner
import media.qimeng.next.sdk.model.V2PlanCreateMediaMappingsInner
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.asRequestBody
import java.io.File
import java.io.FileOutputStream
import java.security.MessageDigest

class LegacyBackupRepository internal constructor(private val context: Context, private val provider: ApiProvider) {
    suspend fun stage(target: Session, chosen: Uri, source: String): LegacyDocument = withContext(Dispatchers.IO) {
        require(chosen.scheme == "content" && source.isNotBlank()) { "请选择 v2 文档并填写其来源安装标识" }
        val snapshot = File.createTempFile("v2-input-", ".json", context.cacheDir)
        try {
            val hash = MessageDigest.getInstance("SHA-256")
            var size = 0L
            val input = context.contentResolver.openInputStream(chosen) ?: error("无法打开所选文档")
            input.use { incoming -> FileOutputStream(snapshot).use { output ->
                val buffer = ByteArray(128 * 1024)
                while (true) {
                    currentCoroutineContext().ensureActive()
                    val count = incoming.read(buffer)
                    if (count < 0) break
                    size += count
                    require(size <= 64L * 1024 * 1024) { "v2 文档超过 64 MiB 导入上限" }
                    output.write(buffer, 0, count); hash.update(buffer, 0, count)
                }
                output.fd.sync()
            } }
            val digest = hash.digest().joinToString("") { "%02x".format(it) }
            val key = operationKey("v2-stage", source, digest)
            val record = provider.api(target).stageV2Import(key, source.trim(), snapshot.asRequestBody("application/octet-stream".toMediaType())).bodyOrThrow()
            check(record.sha256 == digest && record.bytes == size && record.sourceId == source.trim()) { "服务器暂存身份与所选文档不一致" }
            LegacyDocument(record.id,record.sourceId,record.sha256,record.mediaCount,record.authorCount,record.eventCount)
        } finally { snapshot.delete() }
    }
    suspend fun prepare(target: Session, doc: LegacyDocument, media: Map<String, String> = emptyMap(), authors: Map<String, String> = emptyMap(), key: String): MigrationPreview {
        val input = V2PlanCreate(media.map { V2PlanCreateMediaMappingsInner(it.key, it.value.trim()) },
            authors.filterValues(String::isNotBlank).map { V2PlanCreateAuthorMappingsInner(it.key, it.value.trim()) })
        return provider.api(target).prepareV2Migration(doc.id, key, input).bodyOrThrow().preview()
    }
    suspend fun rows(target: Session, plan: MigrationPreview, authors: Boolean, cursor: String? = null): Page<MigrationRow> {
        val api = provider.api(target)
        if (authors) {
            val result = api.listV2MigrationAuthors(plan.id, cursor, 40).bodyOrThrow()
            return Page(result.items.map { MigrationRow(it.sourceAuthorId, it.name, it.authorId, it.state.value, it.reason) }, result.nextCursor)
        }
        val result = api.listV2MigrationItems(plan.id, cursor, 40).bodyOrThrow()
        return Page(result.items.map { MigrationRow(it.recordKey, it.fileName, it.assetId, it.state.value, it.reason, it.issues) }, result.nextCursor)
    }
    suspend fun commit(target: Session, plan: MigrationPreview, skipped: Boolean): MigrationPreview =
        provider.api(target).commitV2Migration(plan.id, operationKey("v2-commit", plan.id, plan.fingerprint, skipped.toString()),
            CommitV2MigrationRequest(plan.fingerprint, skipped)).bodyOrThrow().preview()

    private fun V2MigrationPlan.preview() = MigrationPreview(id, importId, fingerprint, state.value, mediaCount, matchedCount, skippedCount, issueCount, checkpointId)
}
internal fun operationKey(prefix: String, vararg fields: String): String = prefix + "-" + MessageDigest.getInstance("SHA-256")
    .digest(fields.joinToString("\u0000").toByteArray(Charsets.UTF_8)).joinToString("") { "%02x".format(it) }
