package media.qimeng.next.ui

import android.graphics.Color as PixelColor
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material3.Surface
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import media.qimeng.next.testsupport.captureCloudScreenshot
import media.qimeng.next.ui.chart.QimengTrendLineChart
import media.qimeng.next.ui.chart.QimengTrendSeries
import org.junit.Rule
import org.junit.Test

/** Real Vico rendering and click targets: two lines must share one chart model and tooltip selection. */
class TrendChartBehaviorTest {
    @get:Rule val compose = createComposeRule()

    @Test fun tapUsesLibraryTargetsAndTogglesBothSeriesValues() {
        compose.setContent {
            QimengTheme {
                CompositionLocalProvider(LocalReducedMotion provides true) {
                    Surface {
                        QimengTrendLineChart(listOf(QimengTrendSeries(listOf(9, 3, 6), "图片"), QimengTrendSeries(listOf(7, 5, 10), "视频")),
                            listOf(Color(0xFF4FC3F7), Color(0xFFFF8A65)), listOf("1-1", "1-2", "1-3"),
                            Modifier.fillMaxWidth().height(240.dp).testTag("trend"))
                    }
                }
            }
        }
        compose.waitForIdle()
        val chart = compose.onNodeWithTag("trend")
        fun chartPixels(): IntArray {
            // PixelCopy uses the chart's actual window coordinates, including the insets.
            val bitmap = chart.captureToImage().asAndroidBitmap()
            return IntArray(bitmap.width * bitmap.height).also {
                bitmap.getPixels(it, 0, bitmap.width, 0, 0, bitmap.width, bitmap.height)
            }
        }
        var before = intArrayOf()
        compose.waitUntil(15_000) {
            before = chartPixels()
            bothLinesVisible(before)
        }
        captureCloudScreenshot("stats-trend-unselected.png")
        chart.performTouchInput { click(center) }
        compose.waitUntil(10_000) { chart.fetchSemanticsNode().config[SemanticsProperties.StateDescription] != "未选择数据点" }
        chart.assert(SemanticsMatcher("both series values are announced") {
            it.config[SemanticsProperties.StateDescription] == "1-2 · 图片 3次 · 视频 5次"
        })
        compose.waitUntil(10_000) {
            val after = chartPixels()
            // Opening the marker changes the chart interior as well as the accessibility text.
            before.indices.count { index -> before[index] != after[index] } >= 80
        }
        captureCloudScreenshot("stats-trend-synthetic.png")
        chart.performTouchInput { click(center) }
        compose.waitUntil(10_000) { chart.fetchSemanticsNode().config[SemanticsProperties.StateDescription] == "未选择数据点" }
    }

    private fun bothLinesVisible(pixels: IntArray): Boolean {
        val blue = pixels.any { PixelColor.blue(it) > 204 && PixelColor.green(it) > 153 && PixelColor.red(it) < 141 }
        val orange = pixels.any { PixelColor.red(it) > 204 && PixelColor.green(it) < 184 && PixelColor.blue(it) < 141 }
        return blue && orange
    }
}
