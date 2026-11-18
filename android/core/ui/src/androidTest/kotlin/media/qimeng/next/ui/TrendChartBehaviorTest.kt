package media.qimeng.next.ui

import android.graphics.Bitmap
import android.graphics.Color as PixelColor
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material3.Surface
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
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
                        QimengTrendLineChart(listOf(QimengTrendSeries(listOf(3, 9, 5), "图片"), QimengTrendSeries(listOf(6, 2, 8), "视频")),
                            listOf(Color(0xFF4FC3F7), Color(0xFFFF8A65)), listOf("1-1", "1-2", "1-3"),
                            Modifier.fillMaxWidth().height(240.dp).testTag("trend"))
                    }
                }
            }
        }
        compose.waitForIdle()
        val chart = compose.onNodeWithTag("trend")
        val bounds = chart.fetchSemanticsNode().boundsInWindow
        fun chartPixels(screen: Bitmap): IntArray {
            // Exclude the system clock, status icons and date labels.
            val x = (bounds.left + bounds.width * 0.15f).toInt()
            val y = (bounds.top + bounds.height * 0.08f).toInt()
            val width = (bounds.width * 0.7f).toInt()
            val height = (bounds.height * 0.8f).toInt()
            return IntArray(width * height).also { screen.getPixels(it, 0, width, x, y, width, height) }
        }
        var before = intArrayOf()
        compose.waitUntil(15_000) {
            captureCloudScreenshot("stats-trend-unselected.png") { screen ->
                before = chartPixels(screen)
                bothLinesVisible(before)
            }
        }
        chart.performTouchInput { click(center) }
        compose.waitUntil(10_000) { chart.fetchSemanticsNode().config[SemanticsProperties.StateDescription] != "未选择数据点" }
        chart.assert(SemanticsMatcher("both series values are announced") {
            val text = it.config[SemanticsProperties.StateDescription]
            text.contains("图片") && text.contains("视频") && text.contains("次")
        })
        compose.waitUntil(10_000) {
            captureCloudScreenshot("stats-trend-synthetic.png") { screen ->
                val after = chartPixels(screen)
                // Opening the marker changes the chart interior as well as the accessibility text.
                before.indices.count { index -> before[index] != after[index] } >= 80
            }
        }
        chart.performTouchInput { click(center) }
        compose.waitUntil(10_000) { chart.fetchSemanticsNode().config[SemanticsProperties.StateDescription] == "未选择数据点" }
    }

    private fun bothLinesVisible(pixels: IntArray): Boolean {
        val blue = pixels.any { PixelColor.blue(it) > 204 && PixelColor.green(it) > 153 && PixelColor.red(it) < 141 }
        val orange = pixels.any { PixelColor.red(it) > 204 && PixelColor.green(it) < 184 && PixelColor.blue(it) < 141 }
        return blue && orange
    }
}
