package media.qimeng.next.model

data class AuthorFragment(val id: String, val name: String, val revision: Long, val authors: Int, val works: Int)
data class AuthorMirror(val revision: Long, val path: String, val mode: String, val fragmentId: String?, val state: String, val failure: String?)
data class AuthorImportDraft(val target: Session, val name: String, val content: String, val revision: Long, val key: String)
data class AuthorImportSummary(val newAuthors: Int, val matched: Int, val changed: Int, val issues: List<String>)
data class MissingAuthorWorks(val total: Int, val examples: List<Pair<String, String>>)
data class MaintenanceFolder(val tree: String? = null, val label: String = "", val automatic: Boolean = false,
    val lastChecked: Long = 0, val message: String? = null, val pending: Boolean = false)
data class LegacyDocument(val id: String, val sourceId: String, val digest: String, val media: Int, val authors: Int, val events: Int)
data class MigrationPreview(val id: String, val documentId: String, val fingerprint: String, val state: String, val media: Int,
    val matched: Int, val skipped: Int, val issues: Int, val checkpoint: String?)
data class MigrationRow(val key: String, val name: String, val targetId: String?, val state: String, val reason: String, val issues: List<String> = emptyList())
data class DeviceDeliverySettings(val deviceId: String, val label: String = "", val address: String = "", val configured: Boolean = false,
    val background: Boolean = false, val destination: String? = null, val notice: String? = null, val replaceAuthorProfiles: Boolean = false)
data class DevicePackage(val id: String, val state: String, val createdAt: String, val results: List<DeviceSegmentResult>)
data class DeviceSegmentResult(val kind: String, val state: String, val reason: String?, val receipt: String?)
