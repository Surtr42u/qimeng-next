package media.qimeng.next.data

import media.qimeng.next.model.ExtractionLayer
import media.qimeng.next.model.ExtractionVocabulary
import media.qimeng.next.model.Session

class ExtractionVocabularyRepository internal constructor(private val provider: ApiProvider) {
    suspend fun read(target: Session): ExtractionVocabulary =
        provider.api(target).getCustomSourceGroups().bodyOrThrow().toExtractionVocabulary()

    suspend fun save(target: Session, revision: Long, edits: ExtractionLayer, key: String): ExtractionVocabulary =
        provider.api(target).setCustomSourceGroups(key, edits.toSourceRuleUpdate(revision)).bodyOrThrow()
            .toExtractionVocabulary()
}
