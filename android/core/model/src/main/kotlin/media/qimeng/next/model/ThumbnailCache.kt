package media.qimeng.next.model

enum class ThumbnailPool { NAS, LOCAL }

data class ThumbnailCacheSelection(val pool: ThumbnailPool, val instanceKey: String)
data class ThumbnailUsage(val files: Long, val bytes: Long)
data class ThumbnailCoverage(val covered: Long, val total: Long)
data class ThumbnailPoolUsage(val selection: ThumbnailCacheSelection?, val usage: ThumbnailUsage)

sealed interface ThumbnailPrefetchStatus {
    data object Idle : ThumbnailPrefetchStatus
    data object WaitingForNetwork : ThumbnailPrefetchStatus
    data class Running(val cached: Long, val total: Long) : ThumbnailPrefetchStatus
    data class Done(val cached: Long, val total: Long, val atCapacity: Boolean) : ThumbnailPrefetchStatus
    data class Failed(val message: String) : ThumbnailPrefetchStatus
}

data class ThumbnailPrefetch(val selection: ThumbnailCacheSelection, val status: ThumbnailPrefetchStatus)
