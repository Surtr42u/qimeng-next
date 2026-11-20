package media.qimeng.next.ui.theme

import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.remember
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import kotlin.math.abs

/**
 * 统计图表系列色（数据可视化专用；M3 语义色板没有对应槽位）。
 *
 * 页面的底色、卡片面、坐标轴、图例文字与选中态都取 `MaterialTheme.colorScheme`；
 * 这里只负责折线与图例圆点的系列色，并且和页面一样从当前生效的色板推导，不含任何字面色值。
 *
 * 推导规则（[deriveStatsSeriesColors]）：
 * 1. 取 primary/secondary/tertiary/error 中彩度最高者作为锚点——本套中性灰主题里
 *    primary/secondary/tertiary 均为灰，锚点落在 error（M3 默认错误红）；将来若换成
 *    有色主题，锚点会随最鲜艳的语义色移动，整套系列色跟着换色相。
 * 2. 锚点色相起算，按 360°/8 均匀铺开 8 个色相（5 个类型/来源 + 3 个小时构成），
 *    任意两档色相差为 45° 的整数倍，不依赖人工枚举的色相表。三系列的类型/小时组
 *    各取连续三档；两系列的来源组取相隔 180° 的两档，避免常规/COS 在折线图上过于接近。
 * 3. 饱和度取锚点的 HSL 饱和度并收敛到 0.45–0.80：当前错误红两档都约 0.70，
 *    系列不会因换档发灰，也不会过饱和。
 * 4. 亮度从 `surface` 反推：以 4.5:1 非文本对比为目标（高于要求的 3:1），
 *    浅色主题向暗端取色、深色主题向亮端取色，再按 0.75–1.25 的系数均匀铺开，
 *    使 8 个系列在卡面上既有足够对比、又互相拉开亮度。
 * 5. 对每个色相在 HSL 亮度 0–1 上二分 18 次，命中目标相对亮度；因此黄/绿等高亮度
 *    色相会自动降亮度，不会像固定 HSL 亮度那样在浅色卡面上对比不足。
 *
 * 当前灰系主题的实测对比（相对卡片面 `surface`，浅色白卡 / 深色卡面）：
 * 浅色 3.76–5.60:1，深色 3.56–5.44:1，全部高于 3:1；任意两档的最小色相差为 45°，
 * 相对亮度比约 1.05–1.53。
 */
@Immutable
data class QimengStatsSeriesColors(
    /** 类型趋势、类型分布：图片。 */
    val image: Color,
    /** 类型趋势、类型分布：视频。 */
    val video: Color,
    /** 类型趋势、类型分布：动图。 */
    val animated: Color,
    /** 来源趋势、来源分布：常规。 */
    val regular: Color,
    /** 来源趋势、来源分布：COS。 */
    val cos: Color,
    /** 浏览回顾 24 小时构成：图片。 */
    val hourImage: Color,
    /** 浏览回顾 24 小时构成：动图。 */
    val hourAnimated: Color,
    /** 浏览回顾 24 小时构成：视频。 */
    val hourVideo: Color,
)

/** 系列总数：5 个类型/来源 + 3 个小时构成。 */
private const val SERIES_SLOT_COUNT = 8

/** 相邻档位的色相间隔：整圆均分。 */
private const val SERIES_HUE_STEP = 360f / SERIES_SLOT_COUNT

/** 系列色相对卡片面的目标对比度；留出余量，确保任何色相都不低于 3:1。 */
private const val SERIES_TARGET_CONTRAST = 4.5f

/** 亮度铺开系数：以目标亮度为中心，最暗 0.75 倍、最亮 1.25 倍。 */
private const val SERIES_LUMINANCE_FLOOR = 0.75f
private const val SERIES_LUMINANCE_SPREAD = 0.50f

/** 饱和度收敛范围：避免中性主题下系列色发灰，也避免高饱和主题下过于刺眼。 */
private const val SERIES_MIN_SATURATION = 0.45f
private const val SERIES_MAX_SATURATION = 0.80f

/** HSL 亮度二分次数；18 次后误差约 1/262144，足够稳定命中目标亮度。 */
private const val LIGHTNESS_SEARCH_STEPS = 18

/** 卡片面相对亮度低于该值时系列向亮端取色；4.5:1 反解的两个方向分界约在 0.175。 */
private const val DARK_SURFACE_LUMINANCE = 0.18f

/** 当前生效的统计系列色；与 [QimengTheme] 使用同一份 [MaterialTheme.colorScheme]。 */
@Composable
fun qimengStatsSeriesColors(): QimengStatsSeriesColors {
    val scheme = MaterialTheme.colorScheme
    return remember(scheme) { deriveStatsSeriesColors(scheme) }
}

/**
 * 从色板推导 8 个系列色。锚点、饱和度、亮度目标全部来自 [scheme]，
 * 不读取任何固定色值；色板变化（例如将来启用动态取色或换品牌色）会直接反映到图表。
 */
private fun deriveStatsSeriesColors(scheme: ColorScheme): QimengStatsSeriesColors {
    val surfaceLuminance = scheme.surface.luminance()
    val surfaceIsDark = surfaceLuminance < DARK_SURFACE_LUMINANCE
    val anchor = listOf(scheme.primary, scheme.secondary, scheme.tertiary, scheme.error)
        .maxByOrNull { it.chroma() } ?: scheme.error
    val anchorHue = anchor.hueDegrees()
    val saturation = anchor.hslSaturation().coerceIn(SERIES_MIN_SATURATION, SERIES_MAX_SATURATION)

    // 浅色卡面亮、系列须变暗；深色卡面暗、系列须变亮。从 WCAG 对比公式反解目标相对亮度。
    val baseTargetLuminance = if (surfaceIsDark) {
        (surfaceLuminance + 0.05f) * SERIES_TARGET_CONTRAST - 0.05f
    } else {
        (surfaceLuminance + 0.05f) / SERIES_TARGET_CONTRAST - 0.05f
    }

    fun slot(index: Int): Color {
        val ramp = SERIES_LUMINANCE_FLOOR + SERIES_LUMINANCE_SPREAD * index / (SERIES_SLOT_COUNT - 1)
        return hslSeriesColor(
            hue = anchorHue + SERIES_HUE_STEP * index,
            saturation = saturation,
            targetLuminance = baseTargetLuminance * ramp,
        )
    }

    return QimengStatsSeriesColors(
        image = slot(0),
        video = slot(1),
        animated = slot(2),
        regular = slot(3),
        cos = slot(7),
        hourImage = slot(4),
        hourAnimated = slot(5),
        hourVideo = slot(6),
    )
}

/** 在固定色相/饱和度下二分 HSL 亮度，使结果相对亮度最接近 [targetLuminance]。 */
private fun hslSeriesColor(hue: Float, saturation: Float, targetLuminance: Float): Color {
    val normalizedHue = ((hue % 360f) + 360f) % 360f
    var low = 0f
    var high = 1f
    repeat(LIGHTNESS_SEARCH_STEPS) {
        val mid = (low + high) / 2f
        if (Color.hsl(normalizedHue, saturation, mid).luminance() < targetLuminance) {
            low = mid
        } else {
            high = mid
        }
    }
    return Color.hsl(normalizedHue, saturation, (low + high) / 2f)
}

/** sRGB 彩度（max−min）；中性灰为 0，越鲜艳越大。 */
private fun Color.chroma(): Float =
    maxOf(red, maxOf(green, blue)) - minOf(red, minOf(green, blue))

/** HSL 饱和度（与 [Color.hsl] 的 S 同一定义）；中性灰为 0，越鲜艳越大。 */
private fun Color.hslSaturation(): Float {
    val brightest = maxOf(red, maxOf(green, blue))
    val darkest = minOf(red, minOf(green, blue))
    val lightness = (brightest + darkest) / 2f
    val denominator = 1f - abs(2f * lightness - 1f)
    return if (denominator <= 0f) 0f else (brightest - darkest) / denominator
}

/** sRGB 色相角（0–360°）；灰阶返回 0，仅作为无色相时的兜底。 */
private fun Color.hueDegrees(): Float {
    val brightest = maxOf(red, maxOf(green, blue))
    val delta = chroma()
    if (delta <= 0f) return 0f
    val hue = when (brightest) {
        red -> 60f * (((green - blue) / delta) % 6f)
        green -> 60f * ((blue - red) / delta + 2f)
        else -> 60f * ((red - green) / delta + 4f)
    }
    return ((hue % 360f) + 360f) % 360f
}
