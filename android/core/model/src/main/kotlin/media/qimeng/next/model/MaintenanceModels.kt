package media.qimeng.next.model

data class AuthorFragment(val id: String, val name: String, val revision: Long, val authors: Int, val works: Int)
data class AuthorMirror(val revision: Long, val path: String, val mode: String, val fragmentId: String?, val state: String, val failure: String?)
data class AuthorImportDraft(val target: Session, val name: String, val content: String, val revision: Long, val key: String)
data class AuthorImportSummary(val newAuthors: Int, val matched: Int, val changed: Int, val issues: List<String>)
data class MissingAuthorWorks(val total: Int, val examples: List<Pair<String, String>>)
data class MaintenanceFolder(val tree: String? = null, val label: String = "", val automatic: Boolean = false,
    val lastChecked: Long = 0, val message: String? = null, val pending: Boolean = false)
data class MaintenanceBackup(val id: String, val state: String, val scope: String, val createdAt: String,
    val storageBytes: Long, val archiveBytes: Long, val exportedFiles: Int, val totalFiles: Int,
    val exportError: String?, val exportMessage: String?)
data class BackupExportPlan(val scope: String, val needed: Boolean, val fits: Boolean, val archiveBytes: Long,
    val requiredBytes: Long, val freeBytes: Long, val reserveBytes: Long)
data class DeviceDeliverySettings(val deviceId: String, val label: String = "", val address: String = "", val configured: Boolean = false,
    val background: Boolean = false, val destination: String? = null, val notice: String? = null, val replaceAuthorProfiles: Boolean = false)
data class DevicePackage(val id: String, val state: String, val createdAt: String, val results: List<DeviceSegmentResult>)
data class DeviceSegmentResult(val kind: String, val state: String, val reason: String?, val receipt: String?,
    val native: DevicePackageBinding? = null, val legacy: LegacyReceiptBinding? = null)
data class DevicePackageBinding(val source: String, val target: String, val packageId: String)
data class LegacyReceiptBinding(val source: String?, val sha256: String?, val target: String?)
sealed interface DeviceReceipt {
    val id: String
    data class Native(override val id: String, val items: List<DeviceReceiptItem>) : DeviceReceipt
    data class Legacy(override val id: String, val state: String, val matched: Int, val media: Int, val skipped: Int, val issues: Int,
        val fingerprint: String, val checkpoint: String?) : DeviceReceipt
}
data class DeviceReceiptItem(val label: String, val kind: String, val block: String?, val state: String, val reason: String?, val receipt: String?, val video: DeviceVideoBinding?)
data class DeviceVideoBinding(val sourceId: String?, val sourceVersion: Long?, val targetId: String?, val targetVersion: Long?, val sha256: String?, val bytes: Long?)
