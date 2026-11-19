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
    private val usageLock = Mutex()
    val state = page.asStateFlow()
    val prefetch = repository.prefetch

    suspend fun observe() = coroutineScope {
        launch {
            graph.settings.transportRevision.collectLatest {
                page.update { it.copy(nas = null, local = null) }
                while (true) {
                    usageLock.withLock { refreshUsage() }
                    delay(5_000L)
                }
            }
        }
        graph.settings.transportRevision.collectLatest {
            page.update { it.copy(coverage = null) }
            while (true) {
                val coverage = read { repository.localCoverage() }
                page.update { it.copy(coverage = coverage) }
                delay(15_000L)
            }
        }
    }

    fun clear(selection: ThumbnailCacheSelection) {
        if (page.value.clearing != null) return
        page.update { it.copy(clearing = selection.pool, error = null) }
        viewModelScope.launch {
            try {
                usageLock.withLock {
                    repository.clear(selection)
                    refreshUsage()
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

    private suspend fun refreshUsage() {
        val nas = read { repository.usage(ThumbnailPool.NAS) }
        val local = read { repository.usage(ThumbnailPool.LOCAL) }
        page.update { it.copy(nas = nas, local = local) }
    }

    private suspend fun <T> read(block: suspend () -> T): T? = try {
        block()
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (_: Exception) {
        null
    }
}
