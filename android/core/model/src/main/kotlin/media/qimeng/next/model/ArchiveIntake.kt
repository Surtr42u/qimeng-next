package media.qimeng.next.model

data class ArchiveRoute(val name: String, val state: String, val libraryId: String?)
data class ArchiveOverview(val id: String, val target: Session, val tree: String, val state: String, val files: Long,
    val queuedFiles: Long, val excludedFolders: Long, val examples: List<String>, val error: String? = null)
data class ArchiveInput(val id: String, val uri: String, val name: String, val size: Long?, val modified: Long?,
    val mediaUri: String? = null, val mediaGeneration: Long? = null, val fileIdentity: String? = null)
data class ArchiveGroup(val id: String, val plan: String, val library: String, val directory: String, val inputs: List<ArchiveInput>)
