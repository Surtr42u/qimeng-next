package media.qimeng.next.screen

import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.snapshotFlow
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.collectLatest
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Asset

/**
 * 视口前瞻预取的并发上限：旧版网格 `gridThumbnailPrefetchPermits = Semaphore(2)`
 * （QimengMediaGrid.kt:77）同值。两两成对意味着滚动中最多两条预热请求在途，
 * 既赶得上可见项的加载又不会抢满局域网带宽。
 */
private const val LOOKAHEAD_CONCURRENCY = 2

/**
 * 滚动中的前瞻预取（旧版 QimengMediaGrid.kt:284-309 同款语义）：可见窗口每次变化
 * （滚动中也算）就为窗口前后各 2 行预热缩略图，按滚动方向排序、2 条并发；
 * 新窗口到来由 collectLatest 取消上一批，不等待滚动落定。旧版证明这是滚入即出图
 * 的关键——落定后再取会让新格先空一个往返。
 */
@Composable
internal fun PrefetchGalleryViewport(
    model: LibraryViewModel,
    grid: LazyGridState,
    sections: List<GallerySection>,
    columns: Int,
) {
    val lifecycleOwner = LocalLifecycleOwner.current
    val transportRevision by model.transportRevision.collectAsStateWithLifecycle()
    val tiles = remember(sections) {
        buildList<Asset?> {
            for (section in sections) {
                if (section.header != null) add(null)
                addAll(section.items)
            }
        }
    }
    LaunchedEffect(lifecycleOwner, grid, tiles, columns, transportRevision) {
        lifecycleOwner.repeatOnLifecycle(Lifecycle.State.STARTED) {
            // 只观察轻量的窗口下标：snapshotFlow 自身按值去重，滚动中每个新窗口都会发射。
            var previousFirst = -1
            snapshotFlow {
                val items = grid.layoutInfo.visibleItemsInfo
                VisibleWindow(first = items.firstOrNull()?.index ?: -1, last = items.lastOrNull()?.index ?: -1)
            }.collectLatest { window ->
                if (window.first < 0) return@collectLatest
                val forward = previousFirst < 0 || window.first >= previousFirst
                previousFirst = window.first
                val nearby = adjacentThumbnails(tiles, window.first, window.last + 1, columns * 2, forward)
                for (pair in nearby.chunked(LOOKAHEAD_CONCURRENCY)) {
                    coroutineScope {
                        pair.map { asset -> async { model.prefetchNearbyThumbnails(listOf(asset)) } }.awaitAll()
                    }
                }
            }
        }
    }
}

/** 可见窗口下标快照。不含滚动标志：滚动中的窗口变化同样要触发前瞻预取。 */
private data class VisibleWindow(val first: Int, val last: Int)

/**
 * 窗口外的候选：行进方向前方 2 行优先，反向 2 行随后（旧版 nearbyThumbnailUrls 的
 * forward/backward 判向口径）。组头占位（null）不参与。
 */
private fun adjacentThumbnails(tiles: List<Asset?>, before: Int, after: Int, limit: Int, forward: Boolean): List<Asset> {
    val start = before.coerceAtMost(tiles.size)
    val end = after.coerceAtMost(tiles.size)
    val ahead = buildList { for (index in end until minOf(end + limit, tiles.size)) tiles[index]?.let(::add) }
    val behind = buildList { for (index in start - 1 downTo maxOf(0, start - limit)) tiles[index]?.let(::add) }
    return if (forward) ahead + behind else behind + ahead
}
