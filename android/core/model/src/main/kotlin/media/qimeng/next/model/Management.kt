package media.qimeng.next.model

data class VocabularyEntry(val id: String, val name: String, val aliases: List<String>, val revision: Long, val sourceId: String? = null)
data class TimelineMark(val id: String, val assetId: String, val positionMs: Long, val label: String,
    val color: String, val contentVersion: Long, val revision: Long)
