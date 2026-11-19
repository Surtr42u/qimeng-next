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
            snapshotFlow {
                val items = grid.layoutInfo.visibleItemsInfo
                val before = items.firstOrNull()?.index ?: return@snapshotFlow emptyList()
                val after = items.last().index + 1
                adjacentThumbnails(tiles, before, after, columns * 2)
            }.collectLatest { nearby ->
                if (nearby.isNotEmpty()) {
                    delay(120)
                    model.prefetchNearbyThumbnails(nearby)
                }
            }
        }
    }
}

private fun adjacentThumbnails(tiles: List<Asset?>, before: Int, after: Int, limit: Int): List<Asset> {
    val start = before.coerceAtMost(tiles.size)
    val end = after.coerceAtMost(tiles.size)
    return buildList {
        for (index in end until minOf(end + limit, tiles.size)) tiles[index]?.let(::add)
        for (index in start - 1 downTo maxOf(0, start - limit)) tiles[index]?.let(::add)
    }
}
