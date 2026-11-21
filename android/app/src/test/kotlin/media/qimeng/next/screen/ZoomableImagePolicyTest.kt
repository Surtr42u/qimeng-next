package media.qimeng.next.screen

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 详情图片手势纯逻辑锁定（口径来源=旧版 feature/detail/image/ZoomImageView.kt）：
 * 双击双向切换 1x ↔ 1.8x、放大态 1.05x 阈值、双指 0.5x–5x、松手低于 1x 回中、
 * 平移按图片（Fit 后）边界钳制、1x 横滑 60dp + 横向占优即切（快甩与慢拖同口径）。
 * 被测函数均为文件级 internal 纯函数、无 Android/Compose 依赖（先例
 * ImageFailureClassifierTest / SiblingSwipePolicyTest）。
 */
class ZoomableImagePolicyTest {

    // ---------- 双击（REQ 2：双向、可重复） ----------

    @Test
    fun `双击从基态放大到 1_8x`() {
        assertEquals(DOUBLE_TAP_ZOOM, zoomTargetForDoubleTap(1f), 0f)
        // 1.1x 以下仍视为基态附近：放大而非回位（旧版严格 > 1.1f 才 resetZoom）
        assertEquals(DOUBLE_TAP_ZOOM, zoomTargetForDoubleTap(1.05f), 0f)
        assertEquals(DOUBLE_TAP_ZOOM, zoomTargetForDoubleTap(DOUBLE_TAP_RESET_THRESHOLD), 0f)
    }

    @Test
    fun `双击在放大态回 1x`() {
        assertEquals(1f, zoomTargetForDoubleTap(1.11f), 0f)
        assertEquals(1f, zoomTargetForDoubleTap(2.5f), 0f)
        assertEquals(1f, zoomTargetForDoubleTap(MAX_ZOOM), 0f)
    }

    @Test
    fun `双击切换可重复且两个方向都成立`() {
        var scale = 1f
        scale = zoomTargetForDoubleTap(scale)
        assertEquals(1.8f, scale, 0f)
        scale = zoomTargetForDoubleTap(scale)
        assertEquals(1f, scale, 0f)
        scale = zoomTargetForDoubleTap(scale)
        assertEquals(1.8f, scale, 0f)
        scale = zoomTargetForDoubleTap(scale)
        assertEquals(1f, scale, 0f)
    }

    // ---------- 缩放范围 / 沉浸阈值 ----------

    @Test
    fun `捏合缩放夹在旧版 0_5x 到 5x`() {
        assertEquals(MIN_GESTURE_ZOOM, clampZoom(0.2f), 0f)
        assertEquals(MAX_ZOOM, clampZoom(9f), 0f)
        assertEquals(2f, clampZoom(2f), 0f)
    }

    @Test
    fun `松手低于 1x 弹回 1x_1x 以上保留`() {
        assertEquals(1f, settleZoom(0.6f), 0f)
        assertEquals(1f, settleZoom(1f), 0f)
        assertEquals(1.8f, settleZoom(1.8f), 0f)
        assertEquals(MAX_ZOOM, settleZoom(MAX_ZOOM), 0f)
    }

    @Test
    fun `放大态阈值严格大于 1_05x`() {
        assertFalse(isZoomImmersive(1f))
        assertFalse(isZoomImmersive(ZOOM_IMMERSIVE_THRESHOLD))
        assertTrue(isZoomImmersive(1.0501f))
        assertTrue(isZoomImmersive(DOUBLE_TAP_ZOOM))
    }

    // ---------- 焦点缩放的平移换算 ----------

    @Test
    fun `从 1x 放大时双击点下的内容保持不动`() {
        // 视口 400 宽、中心 200；双击 x=100；1x → 2x：
        // 平移 = 100-200 - (100-200-0)*2 = 100
        assertEquals(100f, anchoredPan(focus = 100f, center = 200f, fromScale = 1f, toScale = 2f, fromPan = 0f), 0.001f)
        // 复核映射：screen = pan + center + (point-center)*scale = 100+200+(100-200)*2 = 100
        val screen = 100f + 200f + (100f - 200f) * 2f
        assertEquals(100f, screen, 0.001f)
    }

    @Test
    fun `回到 1x 时反向换算抵消放大平移`() {
        assertEquals(
            0f,
            anchoredPan(focus = 100f, center = 200f, fromScale = 2f, toScale = 1f, fromPan = 100f),
            0.001f,
        )
    }

    @Test
    fun `以中心为焦点缩放不产生平移`() {
        assertEquals(0f, anchoredPan(focus = 200f, center = 200f, fromScale = 1f, toScale = 3f, fromPan = 0f), 0.001f)
    }

    // ---------- 平移边界（REQ 4：按图片边界而非视口） ----------

    @Test
    fun `Fit 后内容尺寸按视口比例缩进`() {
        assertEquals(400f, fittedContent(1000f, 500f, 400f, 800f).width, 0.001f)
        assertEquals(200f, fittedContent(1000f, 500f, 400f, 800f).height, 0.001f)
        assertEquals(400f, fittedContent(500f, 1000f, 400f, 800f).width, 0.001f)
        assertEquals(800f, fittedContent(500f, 1000f, 400f, 800f).height, 0.001f)
    }

    @Test
    fun `尺寸未知时按视口兜底`() {
        assertEquals(400f, fittedContent(0f, 0f, 400f, 800f).width, 0.001f)
        assertEquals(800f, fittedContent(0f, 0f, 400f, 800f).height, 0.001f)
    }

    @Test
    fun `图片小于视口时该轴不允许多余平移`() {
        // 横图 Fit 后高 200 < 视口 800：放大 2x 也只有 400 高，纵向必须保持居中
        assertEquals(0f, maxPan(contentSize = 200f, viewportSize = 800f, scale = 2f), 0.001f)
        // 宽 400×2 = 800 > 视口 400：可左右各移动 200
        assertEquals(200f, maxPan(contentSize = 400f, viewportSize = 400f, scale = 2f), 0.001f)
    }

    @Test
    fun `1x 不允许平移_放大后才放开`() {
        assertEquals(0f, maxPan(contentSize = 300f, viewportSize = 400f, scale = 1f), 0.001f)
        assertEquals(0f, maxPan(contentSize = 300f, viewportSize = 400f, scale = 0.5f), 0.001f)
        assertEquals(100f, maxPan(contentSize = 300f, viewportSize = 400f, scale = 2f), 0.001f)
    }

    @Test
    fun `平移每次事件后夹在边界内且贴边反向立即生效`() {
        assertEquals(200f, clampPan(999f, contentSize = 400f, viewportSize = 400f, scale = 2f), 0.001f)
        assertEquals(-200f, clampPan(-999f, contentSize = 400f, viewportSize = 400f, scale = 2f), 0.001f)
        // 已贴右边界后反向 10px：从边界回到 190，不存在先吃掉溢出量的死区
        assertEquals(190f, clampPan(200f - 10f, contentSize = 400f, viewportSize = 400f, scale = 2f), 0.001f)
        assertEquals(0f, clampPan(10f, contentSize = 300f, viewportSize = 400f, scale = 1f), 0.001f)
    }

    // ---------- 1x 横滑切件（REQ 5） ----------

    @Test
    fun `1x 横滑达距离且横向占优即切件_快甩与慢拖同口径`() {
        // 旧版 onFling 要求 |vx|>800，但慢拖没有 fling 时 ACTION_UP 的 swipeDeltaFromDrag
        // 按累积位移判定、不要求速度；两条合并后的契约就是距离 + 横向占优。
        assertEquals(1, swipeDirection(dragX = -100f, dragY = 10f, scale = 1f, distancePx = 80f))
        assertEquals(-1, swipeDirection(dragX = 100f, dragY = 10f, scale = 1f, distancePx = 80f))
        assertEquals(1, swipeDirection(dragX = -200f, dragY = 10f, scale = 1f, distancePx = 80f))
    }

    @Test
    fun `放大态横拖只平移绝不切件`() {
        assertNull(swipeDirection(dragX = -200f, dragY = 0f, scale = 1.2f, distancePx = 80f))
        assertNull(swipeDirection(dragX = -200f, dragY = 0f, scale = 2f, distancePx = 80f))
    }

    @Test
    fun `距离不足或纵向占优不切件`() {
        // 短拖（未达 60dp）不切：防「想点按/轻扫」误翻页。
        assertNull(swipeDirection(dragX = -50f, dragY = 0f, scale = 1f, distancePx = 80f))
        // 纵向/斜向滑动：横向即使过距离也不切。
        assertNull(swipeDirection(dragX = -100f, dragY = 200f, scale = 1f, distancePx = 80f))
        // 严格大于阈值：恰好等于不切（与旧版同级）。
        assertNull(swipeDirection(dragX = -80f, dragY = 0f, scale = 1f, distancePx = 80f))
    }
}
