package media.qimeng.next.model

data class Session(val baseUrl: String, val instanceId: String, val label: String)
enum class ThemeChoice { SYSTEM, LIGHT, DARK }
enum class TabBarMaterial(val label: String) {
    LIQUID("液态玻璃"), FROSTED("磨砂玻璃"), SOLID("纯色坞"), CLASSIC("经典");
    val usesBackdrop: Boolean get() = this == LIQUID || this == FROSTED
}
data class Preferences(val theme: ThemeChoice = ThemeChoice.SYSTEM, val reducedMotion: Boolean = false,
    val material: TabBarMaterial = TabBarMaterial.LIQUID)

data class Library(val id: String, val name: String, val kind: String, val enabled: Boolean, val revision: Long,
    val fileCount: Long, val scanState: String, val latestScanId: String?)
data class Directory(val path: String, val name: String, val fileCount: Long, val hasChildren: Boolean?)
data class DirectoryListing(val items: List<Directory>, val cursor: String?, val directFileCount: Long)
data class MediaCollection(val id: String, val name: String, val kind: String)
data class UploadOptions(val authorIds: List<String> = emptyList(), val collectionId: String? = null, val note: String = "",
    val acquisitionSources: List<String> = emptyList(), val family: String? = null, val authorAcquisitionSources: List<String> = emptyList())
data class Asset(
    val id: String,
    val libraryId: String,
    val target: Session,
    val name: String,
    val originalName: String,
    val relativePath: String,
    val mediaType: String,
    val version: Long,
    val revision: Long,
    val tagsRevision: Long,
    val bytes: Long,
    val width: Int?,
    val height: Int?,
    val durationMs: Long?,
    val authorId: String?,
    val authorIds: List<String>,
    val authorsRevision: Long,
    val authorName: String?,
    val tags: List<String>,
    val favorite: Boolean,
    val liked: Boolean,
    val likeCount: Long,
    val positionMs: Long,
    val positionVersion: Long,
    val note: String,
    val sourceIds: List<String>,
    val characterIds: List<String>,
    val acquisitionSources: List<String>,
    val collectionId: String?,
    val page: Int?,
    val contentUrl: String,
    val thumbnailUrl: String,
    val addedAt: String,
    val rankingScore: Long? = null,
    val fileDate: String? = null,
    val workGroups: List<AssetGroup> = emptyList(),
    val roleGroups: List<AssetGroup> = emptyList(),
    val playCount: Long? = null,
    val lastViewedAt: String? = null,
    val cosWorkName: String? = null,
    val sourceName: String? = null,
)
data class AssetGroup(val key: String, val name: String)
data class AssetQuery(
    val text: String = "",
    val libraryId: String? = null,
    val mediaType: String? = null,
    val kind: String? = null,
    val authorId: String? = null,
    val tag: String? = null,
    val favorite: Boolean? = null,
    val liked: Boolean? = null,
    val sort: String = "default",
    val source: String = "assets",
    val seed: Long = 1,
    val period: String = "week",
    val collectionId: String? = null,
    val order: String = "desc",
    val workKeys: List<String> = emptyList(),
    val roleKeys: List<String> = emptyList(),
    val tags: List<String> = emptyList(),
    val tagMode: String = "any",
    val history: Boolean? = null,
    val viewRange: String = "all",
    val playRange: String = "all",
    val sizeRange: String = "all",
    val timeRange: String = "all",
    val fromYear: Int? = null,
    val toYear: Int? = null,
)
data class DateCount(val date: String, val count: Long)
data class Page<T>(val items: List<T>, val nextCursor: String?, val totalMatched: Long? = null, val dateCounts: List<DateCount> = emptyList(),
    val unknownDateCount: Long? = null)
data class FacetChoice(val key: String, val kind: String, val id: String, val name: String, val count: Long)
data class FacetResult(val items: List<FacetChoice>, val nextCursor: String?, val totalMatched: Long, val totalOptions: Long)
data class SearchWord(val id: String, val type: String, val name: String)
data class Author(val id: String, val name: String, val kind: String, val aliases: List<String>, val biography: String, val sources: List<String>, val followed: Boolean, val revision: Long,
    val fileCount: Long? = null, val viewCount: Long? = null, val acquisitionSources: List<String> = emptyList()) : java.io.Serializable
data class TaskItem(val id: String, val fileName: String, val state: String, val message: String, val assetId: String?)
data class TaskReport(val id: String, val revision: Long, val mode: String, val state: String, val summary: String, val createdAt: String, val items: List<TaskItem>)
data class AuthorTaskReport(val id: String, val authorId: String, val state: String, val allowedFields: List<String>,
    val candidateBlocks: List<String>, val allowClear: Boolean, val maxUpdates: Int, val usedUpdates: Int, val createdAt: String, val expiresAt: String)
data class HistoryEntry(val id: String, val assetId: String, val name: String, val kind: String, val positionMs: Long, val createdAt: String,
    val asset: Asset? = null, val contentVersion: Long = 0)
data class InventoryStats(val assets: Long, val images: Long, val videos: Long, val bytes: Long, val favorites: Long, val authors: Long, val events: Long, val trash: Long)
data class ServerCapabilities(val aiConfigured: Boolean, val recommendations: Boolean, val rankings: Boolean)

enum class UploadMode { ORDINARY, INTELLIGENT }
enum class TransferState { QUEUED, UPLOADING, WAITING, SUCCEEDED, FAILED, CANCELLED, REJECTED }

fun TransferState.isFinished() = this == TransferState.SUCCEEDED || this == TransferState.CANCELLED || this == TransferState.REJECTED

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
    val sourceMtime: String?,
    val batchKey: String,
    val batchLabel: String,
    val relativeDirectory: String = "",
    val uploadId: String? = null,
    val offset: Long = 0,
    val state: TransferState = TransferState.QUEUED,
    val assetId: String? = null,
    val batchId: String? = null,
    val message: String = "等待传输",
    val options: UploadOptions = UploadOptions(),
    val sourceHash: String? = null,
    val originalName: String? = null,
    val cancelConfirmed: Boolean = false,
    val archiveSourceState: String? = null,
)
data class PendingEvent(val id: String, val target: Session, val assetId: String, val kind: String, val positionMs: Long, val contentVersion: Long,
    val sessionId: String, val seconds: Double = 0.0, val occurredAt: String, val failure: String? = null)
data class UploadDraft(val id: String, val name: String, val mimeType: String, val bytes: Long, val spoolName: String, val sourceHash: String, val originalName: String,
    val sourceMtime: String? = null, val intakeGroup: String? = null)
data class IntakeFailure(val id: String, val name: String, val message: String, val batchKey: String? = null)
/** Original picker selection, recorded before byte reads; never reconstructed from successful uploads. */
data class OriginalInput(val id: String, val originalFileName: String? = null, val size: Long? = null, val expectedHash: String? = null,
    val nameState: String = "unavailable", val intakeGroup: String? = null)
data class TransferBatch(val key: String, val target: Session, val libraryId: String, val mode: UploadMode, val label: String,
    val inputs: List<OriginalInput>, val serverId: String? = null, val manifestFingerprint: String? = null,
    val relativeDirectory: String? = null, val options: UploadOptions? = null, val inputsChecked: Boolean = false)
data class QueueSnapshot(val transfers: List<Transfer> = emptyList(), val events: List<PendingEvent> = emptyList(), val drafts: List<UploadDraft> = emptyList(),
    val intakeFailures: List<IntakeFailure> = emptyList(), val pendingInputs: List<OriginalInput> = emptyList(), val batches: List<TransferBatch> = emptyList())

fun sameTarget(left: Session, right: Session?): Boolean = right != null &&
    left.baseUrl == right.baseUrl && left.instanceId == right.instanceId

/** An outdated request may not release loading or publish into a newer query. */
class RequestGeneration {
    private var current = 0L
    fun next(): Long = ++current
    fun isCurrent(ticket: Long): Boolean = ticket == current
}

fun canRetry(state: TransferState): Boolean = state == TransferState.FAILED || state == TransferState.WAITING

/** Collection pages and chapters are assigned at completion, so an unresolved earlier item blocks its followers. */
fun collectionPredecessor(task: Transfer, transfers: List<Transfer>): Transfer? {
    val collection = task.options.collectionId ?: return null
    return transfers.asSequence().takeWhile { it.id != task.id }.firstOrNull {
        sameTarget(it.target, task.target) && it.libraryId == task.libraryId && it.options.collectionId == collection &&
            !it.state.isFinished()
    }
}
