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
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Asset
import media.qimeng.next.model.sameTarget

/** 邻位原图预载窗口：前 2 后 1（旧版 DetailPreloadPolicy PRELOAD_FORWARD/BACK）。 */
internal const val ORIGINAL_PRELOAD_FORWARD = 2
internal const val ORIGINAL_PRELOAD_BACK = 1

/** 单张原图预载字节上限 8 MiB（旧版 DetailPreloadPolicy.MAX_ORIGINAL_PREFETCH_BYTES）。 */
internal const val MAX_ORIGINAL_PRELOAD_BYTES = 8L * 1024 * 1024

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
 * 单张邻居是否值得预载（旧版 DetailPreloadPolicy.eligibleTargets 的机会性预取口径）：
 * 同一实例、非视频、字节数已知且不超过 8 MiB，且不是当前件/重复项。
 */
internal fun preloadableOriginal(
    asset: Asset,
    current: Asset,
    maxBytes: Long = MAX_ORIGINAL_PRELOAD_BYTES,
): Boolean =
    asset.id != current.id &&
        sameTarget(asset.target, current.target) &&
        asset.mediaType != "video" &&
        asset.contentUrl.isNotBlank() &&
        asset.bytes in 1..maxBytes

/** 邻位原图预载清单：保持 [LibraryViewModel.detailNeighbourOriginals] 给出的「前 2 后 1」顺序，去重后过滤不可预载项。 */
internal fun originalPreloadTargets(
    candidates: List<Asset>,
    current: Asset,
    maxBytes: Long = MAX_ORIGINAL_PRELOAD_BYTES,
): List<Asset> =
    candidates
        .filter { preloadableOriginal(it, current, maxBytes) }
        .distinctBy { it.id }

/**
 * 邻位原图预载：滑到相邻件时原图已在内存缓存里，不必先看约 1 秒的网格缩略图。
 *
 * - 窗口由 [LibraryViewModel.detailNeighbourOriginals] 定（前 2 后 1，只取已加载清单）；
 * - 串行执行（并发 1）：一张完成再发下一张，避免和当前原图抢带宽；
 * - 内存缓存优先、磁盘缓存禁用（与舞台请求逐字同形，见 [originalImageRequest]）；
 * - 换件时 LaunchedEffect 随键取消，在途请求一并中止，不会把上一窗口继续下完。
 *   失败一律静默：预载是尽力而为，错误不该盖住当前详情。
 */
@Composable
internal fun DetailNeighbourOriginalPreload(model: LibraryViewModel, asset: Asset) {
    val imageLoader = LocalMediaImages.current
    val context = LocalContext.current
    // 窗口清单随列表快照变化（组合期读 value，列表补齐后自动刷新）；结构相同则复用上次结果，
    // 避免 LaunchedEffect 因新 List 实例反复重启。
    val candidates = model.detailNeighbourOriginals(
        asset.id,
        back = ORIGINAL_PRELOAD_BACK,
        forward = ORIGINAL_PRELOAD_FORWARD,
    )
    val targets = remember(candidates, asset.id, asset.version) { originalPreloadTargets(candidates, asset) }
    LaunchedEffect(imageLoader, asset.id, asset.version, targets) {
        for (neighbour in targets) {
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
