package media.qimeng.next.screen

import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.animate
import androidx.compose.animation.core.tween
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculateCentroid
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.util.VelocityTracker
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import kotlin.math.abs
import kotlin.math.hypot
import kotlin.math.min
import kotlinx.coroutines.Job
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import media.qimeng.next.model.Asset
import media.qimeng.next.ui.LocalReducedMotion

// ---------- 手势阈值（旧版 feature/detail/image/ZoomImageView.kt 单源） ----------

/** 双击放大倍数（旧版 DOUBLE_TAP_SCALE） */
internal const val DOUBLE_TAP_ZOOM = 1.8f

/** 双击回位阈值：严格大于此值才回 1x（旧版 onDoubleTap 的 normalizedScale > 1.1f） */
internal const val DOUBLE_TAP_RESET_THRESHOLD = 1.1f

/** 放大态阈值：严格大于此值即视为放大（旧版 ZOOM_IMMERSIVE_THRESHOLD） */
internal const val ZOOM_IMMERSIVE_THRESHOLD = 1.05f

/** 捏合缩放下限（旧版 MIN_SCALE，仅手势中允许；松手回 1x） */
internal const val MIN_GESTURE_ZOOM = 0.5f

/** 捏合缩放上限（旧版 MAX_SCALE/END_MAX_SCALE） */
internal const val MAX_ZOOM = 5f

/** 切件速度阈值 px/s（旧版 SWIPE_VELOCITY） */
internal const val SWIPE_VELOCITY = 800f

/** 切件距离阈值（旧版 SWIPE_DISTANCE_DP = 60dp） */
private val SWIPE_DISTANCE = 60.dp

/** 双击缩放动画时长；旧版 ZoomImageView 为瞬切，动画是本轮明确要求（REQ 8）。 */
private const val DOUBLE_TAP_ANIM_MS = 220

/** 已按 Fit 规则落入视口的图片基准尺寸（scale=1 时实际绘制大小）。 */
internal data class FittedContent(val width: Float, val height: Float)

/** 放大态判定（单源；旧版 isZoomImmersive）。 */
internal fun isZoomImmersive(scale: Float): Boolean = scale > ZOOM_IMMERSIVE_THRESHOLD

/** 手势中的缩放钳制：低于 0.5 或高于 5 立即截断（旧版 scaleBy 边界）。 */
internal fun clampZoom(scale: Float): Float = scale.coerceIn(MIN_GESTURE_ZOOM, MAX_ZOOM)

/** 松手后的缩放：低于 1x 弹回 1x；1x 以上保留（上限已由 [clampZoom] 管住）。 */
internal fun settleZoom(scale: Float): Float = if (scale < 1f) 1f else scale.coerceAtMost(MAX_ZOOM)

/**
 * 双击目标缩放：放大态回 1x，否则放大到 1.8x。双向可重复——每次判定只看当前缩放，
 * 不记忆上一次手势（旧版 onDoubleTap 口径）。
 */
internal fun zoomTargetForDoubleTap(currentScale: Float): Float =
    if (currentScale > DOUBLE_TAP_RESET_THRESHOLD) 1f else DOUBLE_TAP_ZOOM

/**
 * 绕焦点缩放后的平移量（等价旧版 Matrix.postScale(factor, px, py)）：
 * graphicsLayer 以视口中心为缩放原点，要让屏幕坐标 [focus] 处的点保持不动，
 * 新平移 = focus - center - (focus - center - fromPan) * (toScale / fromScale)。
 * 传入 [focus] == center 时结果等于原平移按比例缩放（居中缩放）。
 */
internal fun anchoredPan(
    focus: Float,
    center: Float,
    fromScale: Float,
    toScale: Float,
    fromPan: Float,
): Float {
    if (fromScale <= 0f) return fromPan
    return focus - center - (focus - center - fromPan) * (toScale / fromScale)
}

/**
 * ContentScale.Fit 下的实际内容尺寸：宽或高未知（加载前）时按视口兜底——
 * 这时只能以整屏为基准钳制，原图到达后由调用方按真实尺寸重算。
 */
internal fun fittedContent(
    intrinsicWidth: Float,
    intrinsicHeight: Float,
    viewportWidth: Float,
    viewportHeight: Float,
): FittedContent {
    if (intrinsicWidth <= 0f || intrinsicHeight <= 0f || viewportWidth <= 0f || viewportHeight <= 0f) {
        return FittedContent(viewportWidth, viewportHeight)
    }
    val fit = min(viewportWidth / intrinsicWidth, viewportHeight / intrinsicHeight)
    return FittedContent(intrinsicWidth * fit, intrinsicHeight * fit)
}

/**
 * 单轴最大平移：按**图片边界**（基准内容尺寸 × 缩放）而不是视口边界计算。
 * 图片在该轴上小于视口时返回 0 = 必须居中；放大后才允许多出的部分来回移动。
 */
internal fun maxPan(contentSize: Float, viewportSize: Float, scale: Float): Float =
    ((contentSize * scale - viewportSize) / 2f).coerceAtLeast(0f)

/** 平移钳制：每次事件后都夹到边界内，所以贴边后再反向拖动立即生效、不会积累死位移。 */
internal fun clampPan(pan: Float, contentSize: Float, viewportSize: Float, scale: Float): Float {
    val limit = maxPan(contentSize, viewportSize, scale)
    return pan.coerceIn(-limit, limit)
}

/**
 * 1x 横滑切件判定（旧版 ZoomImageView onFling / UP 慢拖合流口径）：
 * - 放大态（>1.05x）一律不切：横拖属于平移；
 * - 距离必须严格超过 60dp 且横向占优（纵向斜滑、下拉不切件）；
 * - 快甩（|vx|>800）额外要求横向速度也占优：快速斜甩不得误判成切件。
 * 方向沿用旧版：手指左移 (dragX<0) → +1 下一张，右移 → -1 上一张。
 */
internal fun swipeDirection(
    dragX: Float,
    dragY: Float,
    velocityX: Float,
    velocityY: Float,
    scale: Float,
    distancePx: Float,
    velocityThreshold: Float = SWIPE_VELOCITY,
): Int? {
    if (isZoomImmersive(scale)) return null
    if (abs(dragX) <= distancePx) return null
    if (abs(dragX) <= abs(dragY)) return null
    if (abs(velocityX) > velocityThreshold && abs(velocityY) > abs(velocityX)) return null
    return if (dragX < 0f) 1 else -1
}

/**
 * 详情图片舞台（替代原 DetailScreen.ImageStage）。
 *
 * 手势结构：**一个** pointerInput 内由两个协作检测器共享同一事件流——
 * [detectTapGestures] 负责单击/双击，同块的 transform 循环负责捏合与拖动。
 * 上一版把两者挂成两个 pointerInput，放大态下 transform 无条件消费每个事件，
 * `detectTapGestures` 的 waitForUpOrCancellation 看到 isConsumed 即取消手势，
 * 双击回调在放大后永远不再触发（用户报「双击放大后再双击不变回去」的根因）。
 * 现在 transform 只在**真正开始拖动**（过 touch slop）或第二根手指按下时消费；
 * 原地轻点在任何缩放态都不消费，双击双向可重复。
 *
 * 口径对齐旧版 ZoomImageView：双击 1.8x/回位阈值 1.1x、双指 0.5x–5x、放大态阈值
 * 1.05x、松手低于 1x 回中、1x 横滑 60dp + 横向占优 + 800px/s、平移按图片边界钳制。
 * 双击缩放动画（220ms）是本轮新要求：旧版 ZoomImageView 用 configureBaseMatrix/
 * postScale 瞬切，源码里没有补间动画。
 */
@Composable
internal fun ZoomableImageStage(
    asset: Asset,
    onNavigate: (Int) -> Unit,
    onReload: () -> Unit,
    modifier: Modifier = Modifier,
    onToggleChrome: () -> Unit,
    onZoomImmersive: (Boolean) -> Unit,
    reset: Int,
) {
    val context = LocalContext.current
    var scale by rememberSaveable(asset.id, asset.version) { mutableFloatStateOf(1f) }
    var panX by rememberSaveable(asset.id, asset.version) { mutableFloatStateOf(0f) }
    var panY by rememberSaveable(asset.id, asset.version) { mutableFloatStateOf(0f) }
    var failure by remember(asset.id) { mutableStateOf<Throwable?>(null) }
    var retry by remember(asset.id) { mutableIntStateOf(0) }
    // 原图真实像素尺寸（Fit 后即基准内容尺寸）；Asset 自带尺寸先占位，原图成功后覆盖。
    var imageWidth by remember(asset.id, asset.version) { mutableIntStateOf(asset.width ?: 0) }
    var imageHeight by remember(asset.id, asset.version) { mutableIntStateOf(asset.height ?: 0) }
    var viewportWidth by remember { mutableIntStateOf(0) }
    var viewportHeight by remember { mutableIntStateOf(0) }
    // 已上报给宿主的沉浸态：只在跨阈值时发一次（旧版 emitZoomImmersive 的边沿语义）。
    var immersiveReported by remember(asset.id, asset.version) { mutableStateOf(false) }
    val animationJob = remember(asset.id, asset.version) { AnimationJob() }

    // pointerInput 长活：回调与配置经 rememberUpdatedState 取最新值，避免旧闭包。
    val currentToggleChrome by rememberUpdatedState(onToggleChrome)
    val currentNavigate by rememberUpdatedState(onNavigate)
    val currentZoomImmersive by rememberUpdatedState(onZoomImmersive)
    val currentReducedMotion by rememberUpdatedState(LocalReducedMotion.current)

    fun contentSize(): FittedContent =
        fittedContent(imageWidth.toFloat(), imageHeight.toFloat(), viewportWidth.toFloat(), viewportHeight.toFloat())

    fun reportImmersive(value: Boolean) {
        if (immersiveReported == value) return
        immersiveReported = value
        currentZoomImmersive(value)
    }

    // 捏合中只放行向上沿；回落只在收束点（松手/动画结束/重置）上报，避免阈值附近闪烁。
    fun reportDuringGesture() {
        if (isZoomImmersive(scale)) reportImmersive(true)
    }

    fun reportSettled() = reportImmersive(isZoomImmersive(scale))

    suspend fun animateTransform(targetScale: Float, targetPanX: Float, targetPanY: Float) {
        val startScale = scale
        val startPanX = panX
        val startPanY = panY
        if (currentReducedMotion) {
            scale = targetScale
            panX = clampPan(targetPanX, contentSize().width, viewportWidth.toFloat(), targetScale)
            panY = clampPan(targetPanY, contentSize().height, viewportHeight.toFloat(), targetScale)
        } else {
            animate(0f, 1f, animationSpec = tween(DOUBLE_TAP_ANIM_MS, easing = FastOutSlowInEasing)) { fraction, _ ->
                val nextScale = startScale + (targetScale - startScale) * fraction
                val content = contentSize()
                scale = nextScale
                panX = clampPan(startPanX + (targetPanX - startPanX) * fraction, content.width, viewportWidth.toFloat(), nextScale)
                panY = clampPan(startPanY + (targetPanY - startPanY) * fraction, content.height, viewportHeight.toFloat(), nextScale)
                // 跨过放大阈值的那一帧立即隐藏 chrome（旧版 emitZoomImmersive 的向上沿语义）；
                // 回落仍只在动画结束（收束点）上报，避免闪烁。
                reportDuringGesture()
            }
        }
        reportSettled()
    }

    // 换图/恢复保存态时同步一次：进程重建后 saveable 缩放可能 >1，宿主需知道仍处放大态。
    LaunchedEffect(asset.id, asset.version) {
        immersiveReported = isZoomImmersive(scale)
        currentZoomImmersive(immersiveReported)
    }
    // 返回键优先退出缩放（宿主 resetImage++）：立即回基态并恢复 chrome。
    LaunchedEffect(reset, asset.id, asset.version) {
        if (reset <= 0) return@LaunchedEffect
        animationJob.job?.cancel()
        scale = 1f
        panX = 0f
        panY = 0f
        reportImmersive(false)
    }
    // 视口或原图尺寸变化后按真实图片边界重新夹紧，避免旋转/大图到达后停在越界位置。
    LaunchedEffect(viewportWidth, viewportHeight, imageWidth, imageHeight) {
        val content = contentSize()
        panX = clampPan(panX, content.width, viewportWidth.toFloat(), scale)
        panY = clampPan(panY, content.height, viewportHeight.toFloat(), scale)
    }

    Box(
        modifier = modifier
            .onSizeChanged {
                viewportWidth = it.width
                viewportHeight = it.height
            }
            .pointerInput(asset.id, asset.version) {
                val swipeDistancePx = SWIPE_DISTANCE.toPx()
                coroutineScope {
                    launch {
                        detectTapGestures(
                            onTap = { currentToggleChrome() },
                            onDoubleTap = { position ->
                                animationJob.job?.cancel()
                                val targetScale = zoomTargetForDoubleTap(scale)
                                val content = contentSize()
                                val viewportW = size.width.toFloat()
                                val viewportH = size.height.toFloat()
                                // 1x 回位：居中并清空平移；放大：锚定双击点（旧版 scaleTo(px,py)）。
                                val targetPanX = if (targetScale <= 1f) 0f else clampPan(
                                    anchoredPan(position.x, viewportW / 2f, scale, targetScale, panX),
                                    content.width, viewportW, targetScale,
                                )
                                val targetPanY = if (targetScale <= 1f) 0f else clampPan(
                                    anchoredPan(position.y, viewportH / 2f, scale, targetScale, panY),
                                    content.height, viewportH, targetScale,
                                )
                                animationJob.job = launch { animateTransform(targetScale, targetPanX, targetPanY) }
                            },
                        )
                    }
                    launch {
                        awaitEachGesture {
                            val down = awaitFirstDown(requireUnconsumed = false)
                            animationJob.job?.cancel()
                            // 动画被新手势打断时按当前真实缩放校正一次沉浸态（例如回位动画到一半）。
                            reportSettled()
                            val tracker = VelocityTracker()
                            tracker.addPosition(down.uptimeMillis, down.position)
                            var dragX = 0f
                            var dragY = 0f
                            var pastSlop = false
                            var transformed = false
                            do {
                                val event = awaitPointerEvent()
                                val pan = event.calculatePan()
                                event.changes.firstOrNull()?.let { tracker.addPosition(it.uptimeMillis, it.position) }
                                dragX += pan.x
                                dragY += pan.y
                                if (!pastSlop && hypot(dragX, dragY) > viewConfiguration.touchSlop) pastSlop = true
                                if (event.changes.count { it.pressed } >= 2) {
                                    // 双指：缩放+焦点平移。焦点取上一帧质心 = 手指下的内容点保持不动。
                                    transformed = true
                                    val previousScale = scale
                                    // 双指重合时 calculateZoom 可能给 NaN：保持原缩放，避免整层数值污染。
                                    val zoomChange = event.calculateZoom().takeIf { it.isFinite() } ?: 1f
                                    val nextScale = clampZoom(previousScale * zoomChange)
                                    val focal = event.calculateCentroid(useCurrent = false)
                                    val content = contentSize()
                                    val ratio = if (previousScale > 0f) nextScale / previousScale else 1f
                                    val centerX = size.width / 2f
                                    val centerY = size.height / 2f
                                    scale = nextScale
                                    panX = clampPan(
                                        focal.x - centerX - (focal.x - centerX - panX) * ratio + pan.x,
                                        content.width, size.width.toFloat(), nextScale,
                                    )
                                    panY = clampPan(
                                        focal.y - centerY - (focal.y - centerY - panY) * ratio + pan.y,
                                        content.height, size.height.toFloat(), nextScale,
                                    )
                                    reportDuringGesture()
                                    event.changes.forEach { it.consume() }
                                } else if (pastSlop && isZoomImmersive(scale)) {
                                    // 放大态单指拖动=平移；未过 slop 的轻点不消费，双击仍可成立。
                                    transformed = true
                                    val content = contentSize()
                                    panX = clampPan(panX + pan.x, content.width, size.width.toFloat(), scale)
                                    panY = clampPan(panY + pan.y, content.height, size.height.toFloat(), scale)
                                    event.changes.forEach { it.consume() }
                                } else if (pastSlop) {
                                    // 1x 横滑候选：过 slop 即消费，tap 检测器据此取消点击。
                                    event.changes.forEach { it.consume() }
                                }
                            } while (event.changes.any { it.pressed })

                            val settled = settleZoom(scale)
                            if (settled != scale) {
                                // 捏合越过 1x：松手弹回基态并居中（旧版 clampScaleEnd 归位）。
                                animationJob.job?.cancel()
                                animationJob.job = launch { animateTransform(settled, 0f, 0f) }
                            } else {
                                val content = contentSize()
                                panX = clampPan(panX, content.width, size.width.toFloat(), scale)
                                panY = clampPan(panY, content.height, size.height.toFloat(), scale)
                                // 纯轻点不改变缩放，不在收束点重报：否则会与 tap 检测器刚触发的
                                // 双击放大上报（true）抢跑，把沉浸态瞬时打回 false。
                                if (transformed) reportSettled()
                            }
                            if (!transformed && pastSlop) {
                                val velocity = tracker.calculateVelocity()
                                swipeDirection(dragX, dragY, velocity.x, velocity.y, scale, swipeDistancePx)?.let {
                                    currentNavigate(it)
                                }
                            }
                        }
                    }
                }
            },
        contentAlignment = Alignment.Center,
    ) {
        // 缩放在同一层里同时作用于缩略图与原图：原图未到时不至于只有底图不跟随。
        Box(
            Modifier
                .fillMaxSize()
                .graphicsLayer {
                    scaleX = scale
                    scaleY = scale
                    translationX = panX
                    translationY = panY
                },
        ) {
            // 直接用网格同一份 Asset 缩略图请求，命中内存缓存；原图加载完成后覆盖。
            AsyncImage(
                model = asset,
                imageLoader = LocalMediaImages.current,
                contentDescription = null,
                contentScale = ContentScale.Fit,
                modifier = Modifier.fillMaxSize(),
            )
            AsyncImage(
                model = remember(asset.contentUrl, asset.version, retry) { originalImageRequest(context, asset) },
                imageLoader = LocalMediaImages.current,
                contentDescription = asset.name,
                contentScale = ContentScale.Fit,
                onError = { failure = it.result.throwable },
                onSuccess = {
                    failure = null
                    val image = it.result.image
                    if (image.width > 0 && image.height > 0) {
                        imageWidth = image.width
                        imageHeight = image.height
                    }
                },
                modifier = Modifier.fillMaxSize(),
            )
        }
        failure?.let { cause ->
            // 失败归因分档：网络抖动与文件真解码不了给不同指引（旧版 ImageLoadErrorClassifier 口径）。
            Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(imageFailureMessage(classifyImageFailure(cause)), style = MaterialTheme.typography.bodyMedium)
                FilledTonalButton(onClick = { retry++; failure = null; onReload() }) { Text("重试") }
            }
        }
    }
}

/** 动画任务单槽：新手势/返回/换图时取消仍在跑的双击动画。 */
private class AnimationJob {
    var job: Job? = null
}
