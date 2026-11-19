package media.qimeng.next.model

data class ExtractionRole(val canonical: String, val aliases: List<String> = emptyList())
data class ExtractionGroup(
    val canonical: String,
    val variants: List<String> = emptyList(),
    val roles: List<ExtractionRole> = emptyList(),
)
data class ExtractionLayer(val groups: List<ExtractionGroup> = emptyList(), val stopWords: List<String> = emptyList())
data class ExtractionVocabulary(
    val layer: ExtractionLayer,
    val revision: Long,
    val rebuildState: String,
    val processed: Long,
    val matched: Long,
    val ambiguous: Long,
    val errorCode: String?,
) {
    val rebuilding: Boolean get() = rebuildState == "pending" || rebuildState == "running"
}
