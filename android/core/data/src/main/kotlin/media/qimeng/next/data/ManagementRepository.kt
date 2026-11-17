package media.qimeng.next.data

import media.qimeng.next.model.*
import media.qimeng.next.sdk.model.CreateTagRequest
import media.qimeng.next.sdk.model.RenameTagRequest
import media.qimeng.next.sdk.model.TermInput
import media.qimeng.next.sdk.model.TimelineInput
import java.util.UUID

class ManagementRepository internal constructor(private val settings: SecureSettings, private val provider: ApiProvider) {
    private fun api() = provider.api(settings.session.value ?: error("请先登录服务器"))
    suspend fun vocabulary(kind: String, cursor: String?): Page<VocabularyEntry> = when (kind) {
        "tags" -> api().listTagVocabulary(cursor = cursor, limit = 50).bodyOrThrow().let { Page(it.items.map { t -> VocabularyEntry(t.name, t.name, emptyList(), t.revision) }, it.nextCursor) }
        "sources" -> api().listSources(cursor = cursor, limit = 50).bodyOrThrow().let { Page(it.items.map { t -> VocabularyEntry(t.id, t.name, t.aliases, t.revision) }, it.nextCursor) }
        "characters" -> api().listCharacters(cursor = cursor, limit = 50).bodyOrThrow().let { Page(it.items.map { t -> VocabularyEntry(t.id, t.name, t.aliases, t.revision, t.sourceId) }, it.nextCursor) }
        else -> error("未知词表")
    }

    suspend fun saveVocabulary(kind: String, entry: VocabularyEntry?, name: String, aliases: List<String>, sourceId: String?) {
        val key = UUID.randomUUID().toString()
        if (kind == "tags") {
            if (entry == null) api().createTag(key, CreateTagRequest(name)).bodyOrThrow()
            else api().renameTag(tag = entry.name, idempotencyKey = key, renameTagRequest = RenameTagRequest(name, entry.revision)).bodyOrThrow()
            return
        }
        val input = TermInput(name = name, aliases = aliases, sourceId = sourceId, expectedRevision = entry?.revision)
        when (kind) {
            "sources" -> if (entry == null) api().createSource(key, input).bodyOrThrow() else api().updateSource(entry.id, key, input).bodyOrThrow()
            "characters" -> if (entry == null) api().createCharacter(key, input).bodyOrThrow() else api().updateCharacter(entry.id, key, input).bodyOrThrow()
            else -> error("未知词表")
        }
    }

    suspend fun deleteTag(tag: VocabularyEntry) {
        val result = api().deleteTag(tag = tag.name, idempotencyKey = UUID.randomUUID().toString(), expectedRevision = tag.revision)
        if (!result.isSuccessful) result.bodyOrThrow()
    }

    suspend fun marks(asset: Asset, cursor: String?): Page<TimelineMark> {
        val page = provider.api(asset.target).listTimelineMarks(id = asset.id, cursor = cursor, limit = 50).bodyOrThrow()
        return Page(page.items.map { TimelineMark(it.id, it.assetId, it.positionMs, it.label, it.color ?: "#6750A4", it.contentVersion, it.revision) }, page.nextCursor)
    }
    suspend fun saveMark(asset: Asset, previous: TimelineMark?, position: Long, label: String, color: String) {
        val api = provider.api(asset.target)
        val input = TimelineInput(positionMs = position, label = label, contentVersion = previous?.contentVersion ?: asset.version,
            color = color, expectedRevision = previous?.revision)
        val key = UUID.randomUUID().toString()
        if (previous == null) api.createTimelineMark(id = asset.id, idempotencyKey = key, timelineInput = input).bodyOrThrow()
        else api.updateTimelineMark(id = asset.id, markId = previous.id, idempotencyKey = key, timelineInput = input).bodyOrThrow()
    }
    suspend fun deleteMark(asset: Asset, mark: TimelineMark) {
        val result = provider.api(asset.target).deleteTimelineMark(id = asset.id, markId = mark.id,
            idempotencyKey = UUID.randomUUID().toString(), expectedRevision = mark.revision)
        if (!result.isSuccessful) result.bodyOrThrow()
    }
}
