package media.qimeng.next.test

import android.graphics.Color
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.lifecycle.ViewModelProvider
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.runBlocking
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.MainActivity
import media.qimeng.next.data.DataGraph
import media.qimeng.next.model.Asset
import media.qimeng.next.model.TabBarMaterial
import media.qimeng.next.model.ThemeChoice
import media.qimeng.next.testsupport.captureCloudScreenshot
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test

/** Uses the disposable cloud catalog with activity today, 14 days ago and 60 days ago. */
class StatisticsAndHomeTest {
    @get:Rule val compose = createAndroidComposeRule<MainActivity>()

    @Test(timeout = 240_000)
    fun windowsAndPartitionsPreserveTheGoResultsInTheAndroidApp() {
        val args = InstrumentationRegistry.getArguments()
        val endpoint = requireNotNull(args.getString("qimengStatisticsUrl"))
        require(endpoint == "http://127.0.0.1:18483/") { "Only the isolated statistics service is allowed" }
        val token = requireNotNull(args.getString("qimengToken"))
        val graph = DataGraph.get(InstrumentationRegistry.getInstrumentation().targetContext)
        val settings = graph.settings
        val previous = settings.session.value
        val previousToken = previous?.let(settings::tokenFor)
        val preferences = settings.preferences.value
        try {
            runBlocking { graph.media.login(endpoint, token) }
            settings.savePreferences(preferences.copy(theme = ThemeChoice.DARK, material = TabBarMaterial.CLASSIC, reducedMotion = true))
            val model = compose.runOnIdle { ViewModelProvider(compose.activity)[LibraryViewModel::class.java] }
            compose.runOnIdle { model.selectTab(0) }
            val libraries = runBlocking { graph.media.libraryPage().items.associate { it.kind to it.id } }
            assertHomePartitions(model, libraries.getValue("regular"), libraries.getValue("cos"))
            assertRetryAndStatistics(model, graph, endpoint, token)
        } finally {
            if (previous == null) runBlocking { graph.media.disconnect() }
            else settings.saveSession(previous, requireNotNull(previousToken))
            settings.savePreferences(preferences)
        }
    }

    private fun assertHomePartitions(model: LibraryViewModel, regular: String, cos: String) {
        val recommendations = entries(model, "home:0", 51)
        assertEquals("regular", compose.runOnIdle { model.query("home:0").kind })
        assertEquals(setOf(regular), recommendations.map { it.libraryId }.toSet())

        compose.onNodeWithText("COS").performClick()
        assertEquals(setOf(cos), entries(model, "home:1", 2).map { it.libraryId }.toSet())
        assertEquals("cos", compose.runOnIdle { model.query("home:1").kind })

        compose.onNodeWithText("排行榜").performClick()
        val rankings = entries(model, "home:2", 51)
        assertEquals("regular", compose.runOnIdle { model.query("home:2").kind })
        assertEquals(setOf(regular), rankings.map { it.libraryId }.toSet())
        assertTrue(rankings.all { it.rankingScore == 1L })

        compose.onNodeWithTag("navigation:albums").performClick()
        val albums = entries(model, "albums", 53)
        assertNull(compose.runOnIdle { model.query("albums").kind })
        assertEquals(mapOf(regular to 51, cos to 2), albums.groupingBy { it.libraryId }.eachCount())
    }

    private fun entries(model: LibraryViewModel, key: String, count: Int): List<Asset> {
        val state = compose.runOnIdle { model.assets(key) }
        compose.waitUntil(25_000) { state.value.loaded || state.value.error != null }
        assertNull(state.value.error)
        if (count > 50) {
            assertEquals(50, state.value.items.size)
            assertNotNull(state.value.cursor)
            compose.onNode(hasScrollToIndexAction() and hasAnyAncestor(hasTestTag("media-browser:$key")), useUnmergedTree = true)
                .performScrollToIndex(48)
            compose.waitUntil(20_000) { state.value.cursor == null || state.value.error != null }
        }
        assertNull(state.value.error)
        assertNull(state.value.cursor)
        assertEquals(count, state.value.items.size)
        assertEquals(count, state.value.items.distinctBy(Asset::id).size)
        return state.value.items
    }

    private fun assertRetryAndStatistics(model: LibraryViewModel, graph: DataGraph, endpoint: String, token: String) {
        val session = requireNotNull(graph.settings.session.value)
        graph.settings.saveSession(session, "synthetic-invalid-statistics-credential")
        compose.onNodeWithTag("navigation:stats").performClick()
        awaitText("登录已失效，请重新登录")
        compose.onNodeWithText("暂无趋势数据").assertDoesNotExist()
        runBlocking { graph.media.login(endpoint, token) }
        compose.onNodeWithText("重试").performClick()

        val windows = listOf(Window("7d", "7天", 52, 8), Window("30d", "30天", 53, 17), Window("all", "全部", 54, 28))
        for ((index, window) in windows.withIndex()) {
            if (index != 0) compose.onNodeWithText(window.label).performClick()
            compose.onNodeWithText(window.label).assertIsSelected()
            compose.waitUntil(20_000) { model.analytics.value.data?.overview?.range == window.range }
            val report = requireNotNull(model.analytics.value.data)
            assertEquals(window.views, report.overview.totals.views)
            assertEquals(window.seconds, report.overview.totals.seconds)
            assertEquals(53L, report.overview.inventory.assets)
            assertEquals(window.views, report.trends.sumOf { it.views })
            assertEquals(mapOf("regular" to 51L, "cos" to 2L), report.overview.sources.associate { it.key to it.assets })
            assertTrue(report.top.getValue("authors").any { it.name == "合成作者" })
            assertTrue(report.top.getValue("tags").any { it.name == "合成标签" })
            assertTrue(report.interests.any { it.name == "合成标签" } || report.interests.any { it.name == "合成作者" })

            assertMetric("总浏览次数", window.views.toString())
            assertMetric("总播放次数", "0")
            assertMetric("总浏览时长", "${window.seconds}秒")
            assertMetric("总文件数", "53")
            assertMetric("总占用空间", "${report.overview.inventory.bytes}B")
            assertMetric("平均浏览次数", "1")
            assertOverviewColorsAndGeometry(window.range)
        }

        openStatisticsCard("分类型趋势 ›")
        awaitText("类型浏览趋势 · 全部")
        compose.onNodeWithContentDescription("浏览趋势", substring = true).assertIsDisplayed()
        captureLightPage("stats-source-trends.png", "类型浏览趋势 · 全部")
        compose.onNodeWithContentDescription("返回").performClick()

        openStatisticsCard("分布统计")
        awaitText("常规 / COS")
        assertMetric("常规 / COS", "51 / 2")
        compose.onNode(hasScrollToIndexAction()).performScrollToNode(hasText("来源分布对比"))
        compose.onNodeWithText("来源分布对比").assertIsDisplayed()
        captureLightPage("stats-partition-distribution.png", "分布统计")
        compose.onNodeWithContentDescription("返回").performClick()

        openStatisticsCard("常看作者与标签")
        awaitText("常看作者与标签 · 全部")
        compose.onNode(hasScrollToIndexAction()).performScrollToNode(hasText("常看标签"))
        compose.onNodeWithText("合成标签").assertIsDisplayed()
        captureLightPage("stats-interests.png", "常看作者与标签 · 全部")
        compose.onNodeWithContentDescription("返回").performClick()

        openStatisticsCard("浏览回顾")
        awaitText("浏览时长")
        for (window in windows.reversed()) {
            compose.onNodeWithText(window.label).performClick()
            compose.waitUntil(20_000) { model.review.value.data?.range == window.range }
            val review = requireNotNull(model.review.value.data)
            assertEquals(window.views, review.viewCount)
            assertEquals(window.seconds, review.totalSeconds)
            assertEquals(window.seconds, review.hours.sumOf { it.seconds })
            compose.onNodeWithContentDescription("24 小时图片、动图和视频停留构成").assertIsDisplayed()
        }
        captureLightPage("stats-browsing-review.png", "浏览回顾")
        compose.onNodeWithContentDescription("返回").performClick()
        compose.onNodeWithTag("navigation:home").performClick()
        assertEquals(ThemeChoice.DARK, graph.settings.preferences.value.theme)
        compose.waitUntil(10_000) {
            captureCloudScreenshot("stats-return-to-dark-home.png") { screen -> screen.getPixel(4, 32) == Color.rgb(20, 20, 20) }
        }
    }

    private fun openStatisticsCard(label: String) {
        compose.onNode(hasScrollToIndexAction()).performScrollToNode(hasText(label))
        compose.onNodeWithText(label).performClick()
    }

    private fun awaitText(label: String) {
        compose.waitUntil(20_000) { compose.onAllNodesWithText(label).fetchSemanticsNodes().isNotEmpty() }
    }

    private fun assertMetric(label: String, value: String) {
        compose.onNode(hasText(value) and hasAnySibling(hasText(label)), useUnmergedTree = true).assertIsDisplayed()
    }

    private fun assertOverviewColorsAndGeometry(range: String) {
        val labels = listOf("总浏览次数", "总播放次数", "总浏览时长", "总文件数", "总占用空间", "平均浏览次数")
        val bounds = labels.map { compose.onNodeWithText(it, useUnmergedTree = true).fetchSemanticsNode().boundsInWindow }
        val rows = bounds.chunked(3)
        assertTrue(rows[0].maxOf { it.bottom } < rows[1].minOf { it.top })
        rows.forEach { row -> assertTrue(row.zipWithNext().all { (left, right) -> left.right < right.left }) }
        val first = bounds.first()
        compose.waitUntil(10_000) {
            captureCloudScreenshot("stats-overview-$range.png") { screen ->
                screen.getPixel(4, first.center.y.toInt()) == Color.rgb(241, 242, 244) &&
                    screen.getPixel(first.center.x.toInt(), first.bottom.toInt() + 3) == Color.WHITE
            }
        }
    }

    private fun captureLightPage(name: String, label: String) {
        val y = compose.onNodeWithText(label).fetchSemanticsNode().boundsInWindow.center.y.toInt()
        compose.waitUntil(10_000) {
            captureCloudScreenshot(name) { screen -> screen.getPixel(4, y) == Color.rgb(241, 242, 244) }
        }
    }

    private data class Window(val range: String, val label: String, val views: Long, val seconds: Long)
}
