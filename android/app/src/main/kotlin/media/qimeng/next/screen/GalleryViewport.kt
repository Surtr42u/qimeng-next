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
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collectLatest
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Asset

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
            // 只观察轻量的窗口下标 + 滚动标志：原实现在 snapshotFlow 里逐帧构造 Asset 列表，
            // 每帧都要做一次深比较；这里由 snapshotFlow 自己按值去重，滚动中转场不会反复重建列表。
            snapshotFlow {
                val items = grid.layoutInfo.visibleItemsInfo
                VisibleWindow(
                    first = items.firstOrNull()?.index ?: -1,
                    last = items.lastOrNull()?.index ?: -1,
                    scrolling = grid.isScrollInProgress,
                )
            }.collectLatest { window ->
                // 滚动中不预取：预取下载/解码会和可见项的加载抢带宽与 CPU，先保证跟手度。
                if (window.scrolling || window.first < 0) return@collectLatest
                delay(120)
                val nearby = adjacentThumbnails(tiles, window.first, window.last + 1, columns * 2)
                if (nearby.isNotEmpty()) model.prefetchNearbyThumbnails(nearby)
            }
        }
    }
}

/** 可见窗口的最小快照：只在首尾下标或滚动标志变化时重新触发预取。 */
private data class VisibleWindow(val first: Int, val last: Int, val scrolling: Boolean)

private fun adjacentThumbnails(tiles: List<Asset?>, before: Int, after: Int, limit: Int): List<Asset> {
    val start = before.coerceAtMost(tiles.size)
    val end = after.coerceAtMost(tiles.size)
    return buildList {
        for (index in end until minOf(end + limit, tiles.size)) tiles[index]?.let(::add)
        for (index in start - 1 downTo maxOf(0, start - limit)) tiles[index]?.let(::add)
    }
}
