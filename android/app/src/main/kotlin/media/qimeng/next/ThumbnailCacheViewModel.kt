package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import media.qimeng.next.model.ThumbnailCacheSelection
import media.qimeng.next.model.ThumbnailCoverage
import media.qimeng.next.model.ThumbnailPool
import media.qimeng.next.model.ThumbnailPoolUsage

internal data class ThumbnailCachePage(
    val nas: ThumbnailPoolUsage? = null,
    val local: ThumbnailPoolUsage? = null,
    val coverage: Pair<ThumbnailCacheSelection, ThumbnailCoverage>? = null,
    val clearing: ThumbnailPool? = null,
    val error: String? = null,
)

internal class ThumbnailCacheViewModel(app: Application) : AndroidViewModel(app) {
    private val graph = (app as QimengApplication).graph
    private val repository = graph.thumbnails
    private val page = MutableStateFlow(ThumbnailCachePage())
    private val nasUsageLock = Mutex()
    private val localUsageLock = Mutex()
    private var observedTransport: Long? = null
    val state = page.asStateFlow()
    val prefetch = repository.prefetch

    suspend fun observe() = coroutineScope {
        graph.settings.transportRevision.collectLatest { revision ->
            // Re-entering the screen re-subscribes to the current revision; only a real
            // connection switch invalidates the numbers already on screen and forces a rescan.
            if (observedTransport != null && observedTransport != revision) {
                page.update { it.copy(nas = null, local = null, coverage = null) }
            }
            observedTransport = revision
            coroutineScope {
                launch { sampleUsage() }
                launch { sampleCoverage() }
            }
        }
    }

    /** Publishes each pool as soon as its own walk ends instead of waiting for both to finish. */
    private suspend fun sampleUsage() = coroutineScope {
        var nasSampled = -1L
        var localSampled = -1L
        while (true) {
            val nasRevision = repository.nasDiskRevision.value
            if (nasRevision != nasSampled) {
                nasSampled = nasRevision
                launch { nasUsageLock.withLock { refreshUsage(ThumbnailPool.NAS) } }
            }
            val localRevision = repository.localDiskRevision.value
            if (localRevision != localSampled) {
                localSampled = localRevision
                launch { localUsageLock.withLock { refreshUsage(ThumbnailPool.LOCAL) } }
            }
            delay(5_000L)
        }
    }

    /** Polls faster while the embedded server is still generating, so the bar moves with the work. */
    private suspend fun sampleCoverage() {
        while (true) {
            val coverage = read { repository.localCoverage() }
            if (coverage != null) page.update { it.copy(coverage = coverage) }
            val generating = coverage != null && coverage.second.covered < coverage.second.total
            delay(if (generating) 4_000L else 15_000L)
        }
    }

    fun clear(selection: ThumbnailCacheSelection) {
        if (page.value.clearing != null) return
        page.update { it.copy(clearing = selection.pool, error = null) }
        viewModelScope.launch {
            try {
                usageLock(selection.pool).withLock {
                    repository.clear(selection)
                    refreshUsage(selection.pool)
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                page.update { it.copy(error = "未能清空缩略图缓存，请核对当前连接后重试。") }
            } finally {
                page.update { it.copy(clearing = null) }
            }
        }
    }

    private fun usageLock(pool: ThumbnailPool) = if (pool == ThumbnailPool.LOCAL) localUsageLock else nasUsageLock

    private suspend fun refreshUsage(pool: ThumbnailPool) {
        val usage = read { repository.usage(pool) } ?: return
        page.update { current ->
            when (pool) {
                ThumbnailPool.NAS -> current.copy(nas = usage)
                ThumbnailPool.LOCAL -> current.copy(local = usage)
            }
        }
    }

    private suspend fun <T> read(block: suspend () -> T): T? = try {
        block()
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (_: Exception) {
        null
    }
}
