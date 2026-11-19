package media.qimeng.next.data

import media.qimeng.next.model.ExtractionGroup
import media.qimeng.next.model.ExtractionLayer
import media.qimeng.next.model.ExtractionRole
import media.qimeng.next.model.ExtractionVocabulary
import media.qimeng.next.model.Session
import media.qimeng.next.sdk.model.SourceRuleCharacter
import media.qimeng.next.sdk.model.SourceRuleGroup
import media.qimeng.next.sdk.model.SourceRuleSettings
import media.qimeng.next.sdk.model.SourceRuleUpdate

class ExtractionVocabularyRepository internal constructor(private val provider: ApiProvider) {
    suspend fun read(target: Session): ExtractionVocabulary = provider.api(target).getCustomSourceGroups().bodyOrThrow().toModel()

    suspend fun save(target: Session, revision: Long, edits: ExtractionLayer, key: String): ExtractionVocabulary {
        val groups = edits.groups.map { group ->
            SourceRuleGroup(canonical = group.canonical, variants = group.variants, characters = group.roles.map {
                SourceRuleCharacter(canonical = it.canonical, aliases = it.aliases)
            })
        }
        val input = SourceRuleUpdate(groups = groups, stopWords = edits.stopWords, expectedRevision = revision)
        return provider.api(target).setCustomSourceGroups(key, input).bodyOrThrow().toModel()
    }
}

private fun SourceRuleSettings.toModel() = ExtractionVocabulary(
    layer = ExtractionLayer(groups = groups.map { group ->
        ExtractionGroup(group.canonical, group.variants, group.characters.map { ExtractionRole(it.canonical, it.aliases) })
    }, stopWords = stopWords),
    revision = revision,
    rebuildState = rebuildState.value,
    processed = processed,
    matched = matched,
    ambiguous = ambiguous,
    errorCode = errorCode,
)
