package media.qimeng.next.screen

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import coil3.request.CachePolicy
import coil3.request.ImageRequest
import coil3.size.Size
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Asset
import media.qimeng.next.model.sameTarget
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/** 邻位原图预载窗口：前 2 后 1（旧版 DetailPreloadPolicy PRELOAD_FORWARD/BACK）。 */
internal const val ORIGINAL_PRELOAD_FORWARD = 2
internal const val ORIGINAL_PRELOAD_BACK = 1

/** 邻位 lg 预览首轮并发上限：2。预览体积小，但仍要给当前正在看的大图留出带宽。 */
internal const val PREVIEW_PRELOAD_CONCURRENCY = 2

/** 单张原图预载字节上限 8 MiB（旧版 DetailPreloadPolicy.MAX_ORIGINAL_PREFETCH_BYTES）。 */
internal const val MAX_ORIGINAL_PRELOAD_BYTES = 8L * 1024 * 1024

/**
 * 单张原图预载的像素上限（约 8 MP）。解码后按 4 B/px 估算：超过这个尺寸的原图
 * 本身就大于 25%×256MB≈64MiB 的 Coil 强缓存，`StrongMemoryCache` 不会收它——
 * 预载只会白耗流量与解码时间，不如滑到跟前再逐张加载。
 */
internal const val MAX_ORIGINAL_PRELOAD_PIXELS = 8_000_000L

/**
 * 详情原图的显式内存缓存键。舞台（[ZoomableImageStage]）与邻位预载都必须用这一个键：
 * 键不一致时预载的解码结果进不了舞台命中的槽位，warm-up 等于白做。
 */
internal fun originalImageMemoryKey(asset: Asset): String =
    "${asset.target.baseUrl}:${asset.target.instanceId}:${asset.id}:${asset.version}:original"

/**
 * 详情原图请求单源（舞台与预载共用）：原始像素、不进磁盘缓存、固定内存键。
 * 旧版预载没有显式键，靠 Coil 默认键（url+size）与舞台自然重合；新仓舞台已带
 * 自定义键，预载必须逐字相同才命中。
 */
internal fun originalImageRequest(context: Context, asset: Asset): ImageRequest =
    ImageRequest.Builder(context)
        .data(asset.contentUrl)
        .size(Size.ORIGINAL)
        .memoryCachePolicy(CachePolicy.ENABLED)
        .diskCachePolicy(CachePolicy.DISABLED)
        .memoryCacheKey(originalImageMemoryKey(asset))
        .build()

/**
 * 详情首帧预览地址：服务端缩略图端点支持 `size=sm|md|lg`（长边 256/512/1024），
 * `Asset.thumbnailUrl` 是已带 `version` 的绝对地址（见 MediaRepository.toModel）。
 * 网格默认的 `md` 在这里换成 `lg`：`HttpUrl.setQueryParameter` 会替换同名参数的
 * 全部既有值，URL 已带 `size=md/sm` 也不会重复；空地址或非法地址返回 null，
 * 舞台退回主题底色，不构建请求也不崩溃。
 */
internal fun detailPreviewUrl(thumbnailUrl: String): String? {
    if (thumbnailUrl.isBlank()) return null
    val url = thumbnailUrl.toHttpUrlOrNull() ?: return null
    return url.newBuilder().setQueryParameter("size", "lg").build().toString()
}

/**
 * 详情 lg 预览的固定内存键：实例地址 + 实例身份 + 资产 + 内容版本 + 用途后缀。
 * 不能沿用网格的 `Keyer<Asset>` 键（那是 512 的 md 位图，满屏拉伸就是用户看到的糊图）；
 * 用固定键而不是 Coil 默认键，保证同一 (实例, 资产, 内容版本) 的 lg 预览跨重组、
 * 复访都命中同一个内存槽，且键里的用途后缀与网格 md 槽、原图槽互不相通。
 */
internal fun detailPreviewMemoryKey(asset: Asset): String =
    "${asset.target.baseUrl}:${asset.target.instanceId}:${asset.id}:${asset.version}:detail-preview"

/**
 * 详情首帧预览请求：lg 缩略图 + 固定内存键 + 显式 Size.ORIGINAL。磁盘缓存保持 Coil 默认
 * （版本号在 URL 里，换内容不会拿到旧字节）；认证仍走应用 ImageLoader 的 mediaClient。
 *
 * 这里的 ORIGINAL 指**预览源**的原尺寸（服务端 lg 长边 1024 封顶），不是媒体原件。
 * 必须显式固定：Coil 内存缓存会拒收小于本次请求解析尺寸的位图——舞台 AsyncImage 若按
 * 满屏（如 1080px）解析，会嫌预载的 1024 位图太小而重新解码，邻位预载就白做。舞台与
 * 预载都只经本函数构请求，URL、内存键、尺寸三者逐字一致，滑到邻位才能直接命中预取图。
 */
internal fun detailPreviewRequest(context: Context, asset: Asset): ImageRequest? {
    val url = detailPreviewUrl(asset.thumbnailUrl) ?: return null
    return ImageRequest.Builder(context)
        .data(url)
        .size(Size.ORIGINAL)
        .memoryCacheKey(detailPreviewMemoryKey(asset))
        .build()
}

/**
 * 邻位 lg 预览准入：同一实例、非视频、非当前件，且缩略图地址能解析出 lg 预览
 * （[detailPreviewUrl] 非 null）。不套用原件的 8 MiB / 8 MP 上限——预览固定 1024 长边，
 * 超大原图恰恰最需要滑到跟前时先有一张清楚的 1024 首帧。
 * 视频不预载：邻位视频由 VideoStage 播放，没有图片舞台可命中。
 */
internal fun preloadablePreview(asset: Asset, current: Asset): Boolean =
    asset.id != current.id &&
        sameTarget(asset.target, current.target) &&
        asset.mediaType != "video" &&
        detailPreviewUrl(asset.thumbnailUrl) != null

/** 邻位 lg 预览预载清单：保持 [neighbourPreloadOrder] 的近者优先顺序，去重后过滤不可预载项。 */
internal fun previewPreloadTargets(
    candidates: List<Asset>,
    current: Asset,
): List<Asset> =
    candidates
        .filter { preloadablePreview(it, current) }
        .distinctBy { it.id }

/**
 * 单张邻居是否值得预载原图（旧版 DetailPreloadPolicy.eligibleTargets 的机会性预取口径）：
 * 同一实例、非视频、字节数已知且不超过 8 MiB、解码后不超过 8 MP，且不是当前件/重复项。
 * 像素这一条是新增的：压缩字节小但分辨率极高的图进不了内存缓存，预载纯属浪费。
 */
internal fun preloadableOriginal(
    asset: Asset,
    current: Asset,
    maxBytes: Long = MAX_ORIGINAL_PRELOAD_BYTES,
    maxPixels: Long = MAX_ORIGINAL_PRELOAD_PIXELS,
): Boolean {
    val pixels = (asset.width ?: 0).toLong() * (asset.height ?: 0)
    return asset.id != current.id &&
        sameTarget(asset.target, current.target) &&
        asset.mediaType != "video" &&
        asset.contentUrl.isNotBlank() &&
        asset.bytes in 1..maxBytes &&
        pixels in 1..maxPixels
}

/**
 * 邻位预载取件顺序（旧版 DetailPreloadPolicy.computePreload 的 orderedDeltas =
 * `listOf(1, -1) + (2..PRELOAD_FORWARD)`，见 DetailPreloadPolicy.kt:56）：
 * [ahead] 是当前件之后由近到远的邻居（+1、+2），[behind] 是之前由近到远的邻居（-1）。
 * 两份清单交错合并成 +1、-1、+2 的近者优先序；按 id 去重，重复项只保留先出现的一次。
 */
internal fun neighbourPreloadOrder(ahead: List<Asset>, behind: List<Asset>): List<Asset> {
    val ordered = ArrayList<Asset>(ahead.size + behind.size)
    val seen = HashSet<String>(ahead.size + behind.size)
    for (distance in 0 until maxOf(ahead.size, behind.size)) {
        ahead.getOrNull(distance)?.let { if (seen.add(it.id)) ordered += it }
        behind.getOrNull(distance)?.let { if (seen.add(it.id)) ordered += it }
    }
    return ordered
}

/** 邻位原图预载清单：按 [neighbourPreloadOrder] 的近者优先顺序（+1、-1、+2），去重后过滤不可预载项。 */
internal fun originalPreloadTargets(
    candidates: List<Asset>,
    current: Asset,
    maxBytes: Long = MAX_ORIGINAL_PRELOAD_BYTES,
): List<Asset> =
    candidates
        .filter { preloadableOriginal(it, current, maxBytes) }
        .distinctBy { it.id }

/**
 * 邻位预载：滑到相邻件时先有清晰的 lg 预览、原图随后已在内存缓存里，不必先看约 1 秒的网格缩略图。
 *
 * - 窗口由 [LibraryViewModel.detailNeighbourOriginals] 定（前 2 后 1，只取已加载清单），
 *   顺序按旧版 computePreload 收成 +1、-1、+2 的近者优先；
 * - 快轮：所有可预览邻居的 lg 预览并发预取（上限 [PREVIEW_PRELOAD_CONCURRENCY]=2），
 *   请求与舞台共用 [detailPreviewRequest]/[detailPreviewMemoryKey]，滑过去立即命中；
 * - 慢轮：原件串行预取（并发 1），一张完成再发下一张，避免和当前原图抢带宽；
 * - 原图内存缓存优先、磁盘缓存禁用（与舞台请求逐字同形，见 [originalImageRequest]）；
 * - 换件时 LaunchedEffect 随键取消，在途的预览与原图请求一并中止，不会把上一窗口继续下完。
 *   失败一律静默：预载是尽力而为，错误不该盖住当前详情。
 */
@Composable
internal fun DetailNeighbourOriginalPreload(model: LibraryViewModel, asset: Asset) {
    val imageLoader = LocalMediaImages.current
    val context = LocalContext.current
    // 前后分开取再按近者优先交错：旧版 computePreload 的顺序是 +1、-1、+2。
    // 窗口清单随列表快照变化（组合期读 value，列表补齐后自动刷新）；结构相同则复用上次结果，
    // 避免 LaunchedEffect 因新 List 实例反复重启。
    val ahead = model.detailNeighbourOriginals(asset.id, back = 0, forward = ORIGINAL_PRELOAD_FORWARD)
    val behind = model.detailNeighbourOriginals(asset.id, back = ORIGINAL_PRELOAD_BACK, forward = 0)
    val candidates = remember(ahead, behind) { neighbourPreloadOrder(ahead, behind) }
    val previews = remember(candidates, asset.id, asset.version) { previewPreloadTargets(candidates, asset) }
    val originals = remember(candidates, asset.id, asset.version) { originalPreloadTargets(candidates, asset) }
    LaunchedEffect(imageLoader, asset.id, asset.version, previews, originals) {
        // 快轮：邻位 lg 预览（并发 2）。舞台用同一个固定内存键读预览，滑到邻位首帧即清晰，
        // 原图仍在下载时也不会先露出主题底色。
        coroutineScope {
            for (group in previews.chunked(PREVIEW_PRELOAD_CONCURRENCY)) {
                group.map { neighbour ->
                    async {
                        try {
                            detailPreviewRequest(context, neighbour)?.let { imageLoader.execute(it) }
                        } catch (cancelled: CancellationException) {
                            throw cancelled
                        } catch (_: Exception) {
                            // 预览失败不打扰当前详情；原件轮仍会尽力预取。
                        }
                    }
                }.awaitAll()
            }
        }
        // 慢轮：串行预取原件（并发 1）。
        for (neighbour in originals) {
            try {
                imageLoader.execute(originalImageRequest(context, neighbour))
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                // 预载失败不打扰当前详情；下一次换件会重新计算窗口。
            }
        }
    }
}
