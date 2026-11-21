package media.qimeng.next.screen

import android.content.Context
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import coil3.ImageLoader
import coil3.request.CachePolicy
import coil3.request.ImageRequest
import coil3.size.Precision
import coil3.size.Scale
import coil3.size.Size
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Asset

/**
 * 视口前瞻预取的并发上限：旧版网格 `gridThumbnailPrefetchPermits = Semaphore(2)`
 * （QimengMediaGrid.kt:77）同值；两条在途足以抢在滚入前一个往返，又不会占满局域网。
 */
private const val LOOKAHEAD_CONCURRENCY = 2

/** 旧版 `nearbyThumbnailUrls` 的窗口宽度上限：`min(columns*2, 16)`（QimengMediaGrid.kt:81）。 */
private const val LOOKAHEAD_TILE_CAP = 16

/** 排队上限：约 4 个前瞻窗口，够两个 worker 持续取活，又不会在慢网下无限堆积。 */
private const val PREFETCH_QUEUE_LIMIT = 64

/**
 * 预热解码尺寸。源图是服务端 md（512 宽）；4-5 列格子约 187-240 px 宽（16:9），
 * 解 256x144 已高于目标，且像素/内存只有 512x288 的四分之一。2-3 列格子约
 * 330-530 px 宽，256 会**小于**目标，Coil 的内存缓存尺寸校验会拒绝这份位图并重新
 * 解码，所以这两档必须按源尺寸预热。
 * 请求侧用 16:9 的 FILL + INEXACT：竖图按宽度解到 256（高度随比例更高），
 * 全景图在 FILL 倍率 >1 时不放大、保留源尺寸（非采样位图，INEXACT 直接接受）。
 * 宽屏平板上 256x144 若小于格子，Coil 拒绝这份缓存并回退磁盘解码——只是少一次
 * 预热，不损画质。
 */
private fun viewportWarmSize(columns: Int): Size =
    if (columns >= 4) Size(256, 144) else Size.ORIGINAL

/**
 * 滚动中的前瞻预取。
 *
 * 预取路径不占用 Main：`snapshotFlow` 读窗口与队列消费都在 Default，下载与解码在
 * 两条长驻 IO worker 上，窗口变化不创建/取消协程，因此不会在帧间向 Main 投递成串
 * 的取消与恢复任务（与设备实测 start_delay p90 51 ms 的帧间主线程工作形态相符）。
 *
 * 预取本身分两步，与旧版 QimengMediaGrid.kt:296-307 的 `imageLoader.execute` 同目的：
 * 1. [LibraryViewModel.prefetchNearbyThumbnails] 把字节写进缩略图磁盘池（不占可见请求的飞行）；
 * 2. 用同一个 ImageLoader 按当前列数解码一次（4-5 列 256x144，2-3 列源尺寸）——磁盘命中
 *    → 解码 → 写入 Coil 内存缓存。`Keyer<Asset>` 的内存键与尺寸无关，AsyncImage 的
 *    INEXACT 精度会接受不小于目标的非采样/大尺寸位图；格子滚入时直接命中内存，
 *    不再在可见帧里读盘+解码。旧版网格预取正是 `imageLoader.execute(...size(Size.ORIGINAL))`
 *    （默认内存缓存开启），所以旧版滚入即出图；新实现此前只落盘，这是「顿一下」的另一半。
 */
@Composable
internal fun PrefetchGalleryViewport(
    model: LibraryViewModel,
    grid: LazyGridState,
    sections: List<GallerySection>,
    columns: Int,
) {
    val lifecycleOwner = LocalLifecycleOwner.current
    val context = LocalContext.current
    val imageLoader = LocalMediaImages.current
    val transportRevision by model.transportRevision.collectAsStateWithLifecycle()
    val tiles = remember(sections) {
        buildList<Asset?> {
            for (section in sections) {
                if (section.header != null) add(null)
                addAll(section.items)
            }
        }
    }
    // 分页会换 sections/tiles，但分页不应取消在途预取：worker 读最新列表，不重启 effect。
    val latestTiles by rememberUpdatedState(tiles)
    LaunchedEffect(lifecycleOwner, grid, columns, transportRevision, imageLoader) {
        // 4-5 列格子在 187-240 px 宽：解码 256x144 已高于目标且小于源图 md 512，
        // 位图更小、解码更短；2-3 列格子 330-530 px 宽，256 会小于目标而被
        // Coil 的缓存尺寸校验拒绝，必须按源尺寸（512）预热。
        val warmSize = viewportWarmSize(columns)
        lifecycleOwner.repeatOnLifecycle(Lifecycle.State.STARTED) {
            // 仅保留最新窗口；生产端在 Default 读快照状态，Main 连 trySend 都不做。
            val windows = Channel<VisibleWindow>(Channel.CONFLATED)
            launch(Dispatchers.Default) {
                snapshotFlow {
                    val items = grid.layoutInfo.visibleItemsInfo
                    VisibleWindow(items.firstOrNull()?.index ?: -1, items.lastOrNull()?.index ?: -1)
                }.collect { windows.trySend(it) }
            }

            val queue = PrefetchQueue()
            val wake = Channel<Unit>(Channel.UNLIMITED)
            // 两条长驻 worker：窗口变化不取消它们，只有离开 STARTED 才结束。
            repeat(LOOKAHEAD_CONCURRENCY) {
                launch(Dispatchers.IO) {
                    while (isActive) {
                        val item = queue.poll()
                        if (item == null) {
                            wake.receive()
                            continue
                        }
                        try {
                            prefetchAndWarm(model, imageLoader, context, item.asset, warmSize)
                        } catch (cancelled: CancellationException) {
                            throw cancelled
                        } catch (_: Exception) {
                            // 单图失败不能让队列停摆；可见项会走自己的请求。
                        } finally {
                            queue.finish(item)
                        }
                    }
                }
            }

            // 窗口计算/去重/剪枝都在 Default，Main 不参与。
            launch(Dispatchers.Default) {
                var previousFirst = -1
                var previousForward: Boolean? = null
                var lastTiles: List<Asset?>? = null
                for (window in windows) {
                    if (window.first < 0) continue
                    val currentTiles = latestTiles
                    if (currentTiles !== lastTiles) {
                        // 重查询换了整张清单：丢掉尚未开始的旧队列（在途项继续跑完）。
                        // 分页只是尾部追加，保留队列；用首个资产身份区分两者。
                        if (lastTiles != null && !appendsTo(lastTiles, currentTiles)) queue.dropQueued()
                        lastTiles = currentTiles
                    }
                    val forward = previousFirst < 0 || window.first >= previousFirst
                    previousFirst = window.first
                    // 反向滚动时旧方向的排队项已不在行进方向上，交给方向反转的
                    // 一次清队列（在途项不取消），新方向的前瞻项立即排到队首。
                    if (previousForward != null && previousForward != forward) queue.dropQueued()
                    previousForward = forward
                    val limit = minOf(columns * 2, LOOKAHEAD_TILE_CAP)
                    // 旧版前瞻窗口严格在行进方向的可见窗口之外（QimengMediaGrid.kt:82）：
                    // 正向滚动时 index<=last（已可见或已滚过）的排队项作废，反向时
                    // index>=first 的作废。只丢排队项，在途/解码中的项不在 queued 里。
                    queue.dropStale(window.first, window.last, forward)
                    var added = false
                    for (item in lookaheadTiles(currentTiles, window.first, window.last + 1, limit, forward)) {
                        when (queue.offer(item)) {
                            QueueOffer.ADDED -> added = true
                            QueueOffer.PRESENT -> Unit
                            QueueOffer.FULL -> break
                        }
                    }
                    // 有新项才唤醒；一次补两个令牌让两条 worker 同时开工。worker 取到
                    // 一项后会继续 poll，所以多余令牌只会在队列空时被快速消费，不会堆积。
                    if (added) repeat(LOOKAHEAD_CONCURRENCY) { wake.trySend(Unit) }
                }
            }
        }
    }
}

/** 可见窗口下标快照。不含滚动标志：滚动中的窗口变化同样要触发前瞻预取。 */
private data class VisibleWindow(val first: Int, val last: Int)

/** 队列项带原始格子下标，剪枝只按位置，不重新扫描整张清单。 */
private data class PrefetchItem(val asset: Asset, val index: Int)

/**
 * 旧版几何（QimengMediaGrid.kt:79-87）：只取行进方向 `min(columns*2,16)` 个格子，
 * 反向不预取（用户反向滚动时下一窗口自然会改为向后取）；组头与动图/空缩略图跳过。
 */
private fun lookaheadTiles(
    tiles: List<Asset?>,
    before: Int,
    after: Int,
    limit: Int,
    forward: Boolean,
): List<PrefetchItem> {
    val start = before.coerceIn(0, tiles.size)
    val end = after.coerceIn(0, tiles.size)
    val indices = if (forward) {
        (end until minOf(end + limit, tiles.size))
    } else {
        (maxOf(0, start - limit) until start).reversed()
    }
    return indices.mapNotNull { index ->
        tiles.getOrNull(index)
            ?.takeIf { it.thumbnailUrl.isNotBlank() && it.mediaType != "animated_image" }
            ?.let { PrefetchItem(it, index) }
    }
}

/** 尾部追加判定：首个资产仍是同一条（分页只 append，不替换前缀）；组头是 null，跳过。 */
private fun appendsTo(previous: List<Asset?>, current: List<Asset?>): Boolean {
    if (previous.isEmpty()) return true
    if (current.size < previous.size) return false
    val previousFirst = previous.firstOrNull { it != null } ?: return false
    val currentFirst = current.firstOrNull { it != null } ?: return false
    return previousFirst.id == currentFirst.id
}

/** 入队结果：新增（需要唤醒 worker）、已在队列/在途（无需重复）、队满（停止本窗口）。 */
private enum class QueueOffer { ADDED, PRESENT, FULL }

/**
 * 排队状态，被 Default 消费者与 IO worker 共享，故加锁。
 * [pending] 覆盖「已排队 + 在途 + 解码中」，同一资产在整条链路完成前不会再次入队。
 */
private class PrefetchQueue {
    private val lock = Any()
    private val queued = ArrayDeque<PrefetchItem>()
    private val pending = HashSet<String>()

    fun offer(item: PrefetchItem): QueueOffer = synchronized(lock) {
        if (queued.size >= PREFETCH_QUEUE_LIMIT) return QueueOffer.FULL
        if (!pending.add(item.asset.id)) return QueueOffer.PRESENT
        queued.addLast(item)
        QueueOffer.ADDED
    }

    /** 取出即视为在途；id 留在 [pending]，直到 [finish]。 */
    fun poll(): PrefetchItem? = synchronized(lock) {
        if (queued.isEmpty()) null else queued.removeFirst()
    }

    /** 只丢尚未开始的排队项：已越过行进方向窗口的项交给可见请求/不再需要。在途项不在 [queued] 里。 */
    fun dropStale(first: Int, last: Int, forward: Boolean) = synchronized(lock) {
        val iterator = queued.iterator()
        while (iterator.hasNext()) {
            val item = iterator.next()
            val stale = if (forward) item.index <= last else item.index >= first
            if (stale) {
                iterator.remove()
                pending.remove(item.asset.id)
            }
        }
    }

    fun dropQueued() = synchronized(lock) {
        for (item in queued) pending.remove(item.asset.id)
        queued.clear()
    }

    fun finish(item: PrefetchItem) = synchronized(lock) {
        pending.remove(item.asset.id)
    }
}

/**
 * 先落盘、再用同一个 ImageLoader 解码进内存缓存。两步都跑在 IO worker 上：
 * `prefetchNearby` 自身整段 `withContext(Dispatchers.IO)`，Coil 请求无 target、
 * 不触发 `needsExecuteOnMainDispatcher`，interceptor/decoder 也显式压到 IO，
 * 因此 Main 在帧间不会碰到 URL/键/解码准备。
 */
private suspend fun prefetchAndWarm(
    model: LibraryViewModel,
    imageLoader: ImageLoader,
    context: Context,
    asset: Asset,
    warmSize: Size,
) {
    if (!model.prefetchNearbyThumbnails(listOf(asset))) return
    val request = ImageRequest.Builder(context)
        .data(asset)
        .size(warmSize)
        .scale(Scale.FILL)
        .precision(Precision.INEXACT)
        .memoryCachePolicy(CachePolicy.ENABLED)
        .interceptorCoroutineContext(Dispatchers.IO)
        .fetcherCoroutineContext(Dispatchers.IO)
        .decoderCoroutineContext(Dispatchers.IO)
        .build()
    try {
        imageLoader.execute(request)
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (_: Exception) {
        // 预热失败不改变可见路径：滚入后 AsyncImage 仍会正常请求。
    }
}
