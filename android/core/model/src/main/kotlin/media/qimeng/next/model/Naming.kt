package media.qimeng.next.model

data class NamingFile(val id: String, val name: String)
data class NamingRequest(val libraryId: String, val directory: String, val family: String, val collectionId: String?,
    val numberClass: String, val note: String, val files: List<NamingFile>)
data class NamingSuggestion(val family: String, val example: String)
data class NamingPosition(val family: String, val directory: String, val nextWork: Int, val currentWork: Int?,
    val nextPage: Int?, val nextChapter: Int?, val numberClass: String)
data class NamingPreview(val names: List<String>, val reserved: Boolean)
data class NamingInspection(val suggestions: List<NamingSuggestion>, val truncated: Boolean,
    val position: NamingPosition?, val preview: NamingPreview?, val unsupportedInputs: Boolean = false) {
    val canSubmit get() = preview != null || unsupportedInputs
}
