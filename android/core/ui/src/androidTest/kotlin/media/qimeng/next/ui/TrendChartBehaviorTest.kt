package media.qimeng.next.ui

import android.graphics.Bitmap
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
import androidx.test.platform.app.InstrumentationRegistry
import media.qimeng.next.ui.chart.QimengTrendLineChart
import media.qimeng.next.ui.chart.QimengTrendSeries
import org.junit.Rule
import org.junit.Test
import java.io.File

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
        // The model transaction and library target cache complete asynchronously with rendering.
        compose.waitUntil(10_000) {
            chart.performTouchInput { click(center) }
            chart.fetchSemanticsNode().config[SemanticsProperties.StateDescription] != "未选择数据点"
        }
        chart.assert(SemanticsMatcher("both series values are announced") {
            val text = it.config[SemanticsProperties.StateDescription]
            text.contains("图片") && text.contains("视频") && text.contains("次")
        })
        media.qimeng.next.testsupport.captureCloudScreenshot("stats-trend-synthetic.png")
        chart.performTouchInput { click(center) }
        chart.assert(SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "未选择数据点"))
    }
}
