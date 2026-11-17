package media.qimeng.next.model

data class Session(val baseUrl: String, val instanceId: String, val label: String)
enum class ThemeChoice { SYSTEM, LIGHT, DARK }
data class Preferences(val theme: ThemeChoice = ThemeChoice.SYSTEM, val reducedMotion: Boolean = false, val glass: Boolean = true)

data class Library(val id: String, val name: String)
data class Asset(
    val id: String,
    val name: String,
    val mediaType: String,
    val version: Long,
    val revision: Long,
    val tagsRevision: Long,
    val bytes: Long,
    val width: Int?,
    val height: Int?,
    val durationMs: Long?,
    val authorId: String?,
    val authorName: String?,
    val tags: List<String>,
    val favorite: Boolean,
    val liked: Boolean,
    val positionMs: Long,
    val note: String,
    val contentUrl: String,
    val thumbnailUrl: String,
    val addedAt: String,
)
data class AssetQuery(
    val text: String = "",
    val libraryId: String? = null,
    val mediaType: String? = null,
    val kind: String? = null,
    val authorId: String? = null,
    val tag: String? = null,
    val favorite: Boolean? = null,
    val liked: Boolean? = null,
    val sort: String = "added_desc",
)
data class Page<T>(val items: List<T>, val nextCursor: String?)
data class Author(val id: String, val name: String, val aliases: List<String>, val biography: String, val sources: List<String>, val followed: Boolean, val revision: Long)
data class TaskItem(val id: String, val fileName: String, val state: String, val message: String, val assetId: String?)
data class TaskReport(val id: String, val mode: String, val state: String, val summary: String, val createdAt: String, val items: List<TaskItem>)
data class HistoryEntry(val id: String, val assetId: String, val name: String, val kind: String, val positionMs: Long, val createdAt: String)
data class InventoryStats(val assets: Long, val images: Long, val videos: Long, val bytes: Long, val favorites: Long, val authors: Long, val events: Long, val trash: Long)
data class ServerCapabilities(val aiConfigured: Boolean, val recommendations: Boolean, val rankings: Boolean)

enum class UploadMode { ORDINARY, INTELLIGENT }
enum class TransferState { QUEUED, UPLOADING, WAITING, SUCCEEDED, FAILED, CANCELLED }

/** A transport record only. The server owns asset identities and all domain decisions. */
data class Transfer(
    val id: String,
    val target: Session,
    val libraryId: String,
    val name: String,
    val mimeType: String,
    val bytes: Long,
    val spoolName: String,
    val mode: UploadMode,
    val sourceMtime: String,
    val batchKey: String,
    val batchLabel: String,
    val relativeDirectory: String = "",
    val uploadId: String? = null,
    val offset: Long = 0,
    val state: TransferState = TransferState.QUEUED,
    val assetId: String? = null,
    val batchId: String? = null,
    val message: String = "等待传输",
)
data class PendingEvent(val id: String, val target: Session, val assetId: String, val kind: String, val positionMs: Long, val contentVersion: Long,
    val failure: String? = null)
data class UploadDraft(val id: String, val name: String, val mimeType: String, val bytes: Long, val spoolName: String)
data class QueueSnapshot(val transfers: List<Transfer> = emptyList(), val events: List<PendingEvent> = emptyList(), val drafts: List<UploadDraft> = emptyList())

fun sameTarget(left: Session, right: Session?): Boolean = right != null &&
    left.baseUrl == right.baseUrl && left.instanceId == right.instanceId

/** An outdated request may not release loading or publish into a newer query. */
class RequestGeneration {
    private var current = 0L
    fun next(): Long = ++current
    fun isCurrent(ticket: Long): Boolean = ticket == current
}

fun canRetry(state: TransferState): Boolean = state == TransferState.FAILED || state == TransferState.WAITING
