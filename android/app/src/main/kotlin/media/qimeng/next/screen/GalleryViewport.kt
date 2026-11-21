package media.qimeng.next.screen

import android.content.Context
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
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
 * 可见窗口播种上限：队列的一半。种子只服务「刚上屏的那一屏」，必须给随后入队的
 * 前瞻窗口留出位置——用户立刻滚动时，真正要看的下一屏不能排在几十个种子后面。
 */
private const val SEED_TILE_LIMIT = PREFETCH_QUEUE_LIMIT / 2

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
 * 条目定位也不在 Main：窗口变化只解析「可见窗口 + 前瞻窗口」的少量槽位
 * （[GalleryTileLayout.slice]，O(log 节数 + 窗口长度)），不再为每次分页在主线程摊平
 * 全部已加载条目（旧 `remember(sections) { buildList { ... } }` 的 O(已加载条目数) 成本）。
 *
 * 预取本身分两步，与旧版 QimengMediaGrid.kt:296-307 的 `imageLoader.execute` 同目的：
 * 1. [LibraryViewModel.prefetchNearbyThumbnails] 把字节写进缩略图磁盘池（不占可见请求的飞行）；
 * 2. 用同一个 ImageLoader 按当前列数解码一次（4-5 列 256x144，2-3 列源尺寸）——磁盘命中
 *    → 解码 → 写入 Coil 内存缓存。`Keyer<Asset>` 的内存键与尺寸无关，AsyncImage 的
 *    INEXACT 精度会接受不小于目标的非采样/大尺寸位图；格子滚入时直接命中内存，
 *    不再在可见帧里读盘+解码。旧版网格预取正是 `imageLoader.execute(...size(Size.ORIGINAL))`
 *    （默认内存缓存开启），所以旧版滚入即出图；新实现此前只落盘，这是「顿一下」的另一半。
 *
 * 预热连续性（不得因分页/重查/池满/清空而静默停止）：
 * - 每份新清单的可见窗口由 [seedTiles] 播种一次。前瞻窗口严格在可见窗口之外（旧版同款
 *   几何），所以列表首次上屏、换查询后这一屏原本没有任何预热路径——搜索/筛选后立刻滑动，
 *   首屏每一格都要冷解码；分页追加不重播（那些格子已经在屏上）。
 * - 队列只丢「尚未开始且已不在行进方向」的项；在途与种子从不取消，离开 STARTED 才整体结束。
 * - 池写满或用户刚清空缓存都不停视口前瞻：见 `ThumbnailCacheRepository.prefetchNearby`。
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
    // 组合里只保留 sections 的引用，不摊平清单：分页每次 append 都会换 sections 实例，
    // 旧实现 `remember(sections) { buildList { ... } }` 每页都要在主线程复制一遍已加载的
    // 全部条目；等长替换时 remember 的 key 比较还会走 GallerySection/Asset 的结构相等。
    // 槽位索引与窗口切片改在 Default 消费循环里按需构建/解析（见 [GalleryTileLayout]）。
    // 分页换 sections 不应取消在途预取：worker 读最新索引，不重启 effect。
    val latestSections by rememberUpdatedState(sections)
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
                            prefetchAndWarm(model, imageLoader, context, item, warmSize)
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
                // 槽位索引随 sections 实例重建：只按节数做累积表（O(节数)），不展开条目。
                // 重建也发生在本协程（Default），帧间主线程不承担分页后的索引工作。
                var layout: GalleryTileLayout? = null
                var layoutSections: List<GallerySection>? = null
                for (window in windows) {
                    if (window.first < 0) continue
                    val currentSections = latestSections
                    var added = false
                    val previousLayout = layout
                    if (previousLayout == null || currentSections !== layoutSections) {
                        val next = GalleryTileLayout(currentSections)
                        // 重查询换了整张清单：丢掉尚未开始的旧队列（在途项继续跑完）。
                        // 分页只是尾部追加，保留队列；用首个资产身份区分两者。
                        val newList = previousLayout == null || previousLayout.size == 0 || !appendsTo(previousLayout, next)
                        if (newList && previousLayout != null) queue.dropQueued()
                        layout = next
                        layoutSections = currentSections
                        // 可见窗口播种（每份新清单一次）：前瞻窗口严格在可见窗口之外
                        // （见 [lookaheadTiles]），所以列表首次上屏或换查询后，屏上这批格子
                        // 没有任何预热路径——它们既是用户立刻会看的，也是下一次返回时最该
                        // 命中的。分页追加不重播（那些格子早就在屏上了）。
                        if (newList) {
                            for (item in seedTiles(next, window.first, window.last, SEED_TILE_LIMIT)) {
                                when (queue.offer(item)) {
                                    QueueOffer.ADDED -> added = true
                                    QueueOffer.PRESENT -> Unit
                                    QueueOffer.FULL -> break
                                }
                            }
                        }
                    }
                    val currentLayout = layout ?: continue
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
                    // 种子例外：它们就是当前可见窗口，正在被看，不能被这条规则剪掉。
                    queue.dropStale(window.first, window.last, forward)
                    for (item in lookaheadTiles(currentLayout, window.first, window.last + 1, limit, forward)) {
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
internal data class PrefetchItem(val asset: Asset, val index: Int, val seed: Boolean = false)

/**
 * 网格槽位索引：每节占 `组头(0/1) + 条目数` 个槽位，与 MediaBrowser 的 LazyVerticalGrid
 * 项序一致（组头是整行 item，条目按 `items()` 展开）。只保存每节起点的累积表
 * （O(节数) 个 Int），条目仍留在 sections 里；按下标解析时二分找到所在节再取条目。
 *
 * 分页每次 append 只重建这张小表，不再把全部已加载条目摊平成 `List<Asset?>`——
 * 后者是 O(已加载条目数) 的分配与复制，之前落在主线程的组合里（`remember(sections)`），
 * 正是滚动/翻页卡顿的来源。
 */
internal class GalleryTileLayout(private val sections: List<GallerySection>) {
    /** `starts[i]` 是第 i 节的第一个槽位；`starts[sections.size]` 等于 [size]。 */
    private val starts = IntArray(sections.size + 1)

    /** 槽位总数（含组头）。 */
    val size: Int

    init {
        var total = 0
        for ((index, section) in sections.withIndex()) {
            starts[index] = total
            total += (if (section.header != null) 1 else 0) + section.items.size
        }
        starts[sections.size] = total
        size = total
    }

    /** 全局槽位 [index] 的资产；组头与越界下标返回 null。 */
    fun assetAt(index: Int): Asset? {
        if (index < 0 || index >= size) return null
        val section = sectionAt(index)
        val offset = index - starts[section]
        val header = if (sections[section].header != null) 1 else 0
        return if (offset < header) null else sections[section].items[offset - header]
    }

    /**
     * 解出 `[from, until)` 的槽位（null=组头），只分配窗口长度大小的列表。
     * 复杂度 O(log 节数 + 窗口长度)，与已加载条目数无关；`tiles[local]` 的全局槽位是
     * `base + local`，调用方据此还原交给 [PrefetchQueue] 的下标。
     */
    fun slice(from: Int, until: Int): TileSlice {
        val start = from.coerceIn(0, size)
        val end = until.coerceIn(start, size)
        if (start >= end) return TileSlice(start, emptyList())
        val tiles = ArrayList<Asset?>(end - start)
        var section = sectionAt(start)
        var offset = start - starts[section]
        var index = start
        while (index < end) {
            val current = sections[section]
            val header = if (current.header != null) 1 else 0
            val slots = header + current.items.size
            // 空节（0 槽）不进 index 也不追加：offset 归零后直接看下一节。
            if (offset < slots) {
                tiles += if (offset < header) null else current.items[offset - header]
                index++
            }
            offset++
            if (offset >= slots) {
                section++
                offset = 0
            }
        }
        return TileSlice(start, tiles)
    }

    /** 第一个资产（跳过组头）：分页判定只看它，不比较整条 Asset。 */
    fun firstAsset(): Asset? = sections.firstNotNullOfOrNull { it.items.firstOrNull() }

    /**
     * 最后一个 `starts[i] <= index` 的节。`starts` 单调不减，空节的起止相同，
     * 因此结果必然满足 `starts[i] <= index < starts[i+1]`，空节会被自动跳过。
     */
    private fun sectionAt(index: Int): Int {
        var low = 0
        var high = sections.size - 1
        var result = 0
        while (low <= high) {
            val mid = (low + high) ushr 1
            if (starts[mid] <= index) {
                result = mid
                low = mid + 1
            } else {
                high = mid - 1
            }
        }
        return result
    }
}

/** 窗口切片：`tiles[local]` 的全局槽位是 `base + local`。 */
internal data class TileSlice(val base: Int, val tiles: List<Asset?>)

/**
 * 可见窗口播种清单：列表首次上屏/换查询时，把当前屏上的格子（组头跳过）按
 * 自上而下的顺序入队。与 [lookaheadTiles] 同样跳过空缩略图与动图（动图格子走原件
 * 兜底请求，预热 md 缩略图命中不到它）。真机可见窗口在 5 列布局下可超过 100 格，
 * 这里按 [limit] 截断，余量留给随后入队的前瞻窗口。
 */
internal fun seedTiles(
    tiles: List<Asset?>,
    first: Int,
    last: Int,
    limit: Int,
): List<PrefetchItem> {
    if (limit <= 0 || tiles.isEmpty()) return emptyList()
    val start = first.coerceIn(0, tiles.size)
    val end = (last + 1).coerceIn(0, tiles.size)
    if (start >= end) return emptyList()
    return (start until minOf(end, start + limit)).mapNotNull { index ->
        tiles.getOrNull(index)
            ?.takeIf { it.thumbnailUrl.isNotBlank() && it.mediaType != "animated_image" }
            ?.let { PrefetchItem(it, index, seed = true) }
    }
}

/**
 * [seedTiles] 的索引版：只解出窗口内至多 [limit] 个槽位（O(log 节数 + limit)），
 * 不物化整张清单；返回项的 [PrefetchItem.index] 仍是全局槽位下标，与扁平版逐项等价。
 */
internal fun seedTiles(
    layout: GalleryTileLayout,
    first: Int,
    last: Int,
    limit: Int,
): List<PrefetchItem> {
    if (limit <= 0 || layout.size == 0) return emptyList()
    val start = first.coerceIn(0, layout.size)
    val end = (last + 1).coerceIn(0, layout.size)
    if (start >= end) return emptyList()
    val slice = layout.slice(start, minOf(end, start + limit))
    return seedTiles(slice.tiles, 0, slice.tiles.size - 1, limit)
        .map { it.copy(index = slice.base + it.index) }
}

/**
 * 旧版几何（QimengMediaGrid.kt:79-87）：只取行进方向 `min(columns*2,16)` 个格子，
 * 反向不预取（用户反向滚动时下一窗口自然会改为向后取）；组头与动图/空缩略图跳过。
 *
 * 扁平清单版保留为 [GalleryTileLayout] 索引版的等价基准（单测逐窗口比对）；生产路径走
 * 索引版，不再物化整张清单。
 */
internal fun lookaheadTiles(
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

/**
 * [lookaheadTiles] 的索引版：几何与扁平版完全一致（旧版 QimengMediaGrid.kt:79-87 同口径），
 * 但只解出行进方向至多 [limit] 个槽位（O(log 节数 + limit)），不物化整张清单。
 */
internal fun lookaheadTiles(
    layout: GalleryTileLayout,
    before: Int,
    after: Int,
    limit: Int,
    forward: Boolean,
): List<PrefetchItem> {
    if (limit <= 0 || layout.size == 0) return emptyList()
    val start = before.coerceIn(0, layout.size)
    val end = after.coerceIn(0, layout.size)
    val from = if (forward) end else maxOf(0, start - limit)
    val until = if (forward) minOf(end + limit, layout.size) else start
    if (from >= until) return emptyList()
    val slice = layout.slice(from, until)
    val order = if (forward) slice.tiles.indices else slice.tiles.indices.reversed()
    return order.mapNotNull { local ->
        slice.tiles[local]
            ?.takeIf { it.thumbnailUrl.isNotBlank() && it.mediaType != "animated_image" }
            ?.let { PrefetchItem(it, slice.base + local) }
    }
}

/** 尾部追加判定：首个资产仍是同一条（分页只 append，不替换前缀）；组头不是资产，跳过。 */
private fun appendsTo(previous: GalleryTileLayout, current: GalleryTileLayout): Boolean {
    if (previous.size == 0) return true
    if (current.size < previous.size) return false
    val previousFirst = previous.firstAsset() ?: return false
    val currentFirst = current.firstAsset() ?: return false
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
            // 种子是当前可见窗口，正在被看：位置剪枝对它们不成立（它们会随 finish 出队）。
            val stale = !item.seed && if (forward) item.index <= last else item.index >= first
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
 *
 * 种子（[PrefetchItem.seed]）跳过落盘步骤：它们就是当前屏上正在被 AsyncImage 加载的
 * 格子，同一 `Keyer<Asset>` 键的 flight 已经在跑，直接 `execute` 会命中那份在途下载 /
 * 已落盘字节，只补一次内存缓存位图。若再走一遍 `prefetchNearby`，同一张图会被前瞻
 * 客户端下载第二次，还占掉两个前瞻槽位。格子被回收导致 AsyncImage 取消时，种子的
 * execute 会在 flight 释放后自行补下，屏幕不会停留在空白。
 */
private suspend fun prefetchAndWarm(
    model: LibraryViewModel,
    imageLoader: ImageLoader,
    context: Context,
    item: PrefetchItem,
    warmSize: Size,
) {
    val asset = item.asset
    if (!item.seed && !model.prefetchNearbyThumbnails(listOf(asset))) return
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
