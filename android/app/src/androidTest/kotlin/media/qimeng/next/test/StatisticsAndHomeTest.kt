package media.qimeng.next.test

import android.graphics.Bitmap
import android.graphics.Color
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.semantics.SemanticsActions
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
        } catch (failure: Throwable) {
            runCatching { check(captureCloudScreenshot("stats-flow-failure.png")) }.exceptionOrNull()?.let(failure::addSuppressed)
            throw failure
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

        openStatisticsCard(model, "分类型趋势 ›", "stats-detail:trends:all")
        awaitText("类型浏览趋势 · 全部")
        val periods = requireNotNull(model.analytics.value.data).trends.size
        val chartList = compose.onNode(hasScrollToIndexAction())
        for (title in listOf("类型浏览趋势", "来源浏览趋势")) {
            val chart = hasContentDescription("$title，共 $periods 个时段")
            chartList.performScrollToNode(chart)
            compose.onNode(chart).assertIsDisplayed()
        }
        chartList.performScrollToIndex(0)
        captureLightPage("stats-source-trends.png", "类型浏览趋势 · 全部")
        compose.onNodeWithContentDescription("返回").performClick()

        openStatisticsCard(model, "分布统计", "stats-detail:distribution:all")
        awaitText("常规 / COS")
        assertMetric("常规 / COS", "51 / 2")
        compose.onNode(hasScrollToIndexAction()).performScrollToNode(hasText("来源分布对比"))
        compose.onNodeWithText("来源分布对比").assertIsDisplayed()
        captureLightPage("stats-partition-distribution.png", "分布统计")
        compose.onNodeWithContentDescription("返回").performClick()

        openStatisticsCard(model, "常看作者与标签", "stats-detail:interests:all")
        awaitText("常看作者与标签 · 全部")
        compose.onNode(hasScrollToIndexAction()).performScrollToNode(hasText("常看标签"))
        compose.onNodeWithText("合成标签").assertIsDisplayed()
        captureLightPage("stats-interests.png", "常看作者与标签 · 全部")
        assertTagSearch(model, graph)
        compose.onNodeWithContentDescription("返回").performClick()

        openStatisticsCard(model, "浏览回顾", "review:all")
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
        assertOrdinaryRouteHeadingFollowsTheme(model, graph)
        assertManualSearch(model)
    }

    /**
     * 任务中心是普通路由：既没有 Statistics provider，也没有包裹它的 Surface。
     * 修好之前这里继承的是 Compose 默认纯黑前景，深色主题下标题黑压深底。
     * 这里直接按标题语义范围采样亮度，不依赖固定字形位置，也不写死主题色板。
     */
    private fun assertOrdinaryRouteHeadingFollowsTheme(model: LibraryViewModel, graph: DataGraph) {
        val settings = graph.settings
        val baseline = settings.preferences.value
        compose.runOnIdle { model.navigate("reports") }
        try {
            // onNodeWithText 默认就是全等匹配（substring = false），这里不需要额外的精确参数。
            val bounds = compose.onNodeWithText("任务中心").assertIsDisplayed()
                .fetchSemanticsNode().boundsInWindow
            // 截图是整块显示，bounds 却是窗口坐标：两者只在窗口铺满显示且原点为 (0,0) 时重合。
            // 窗口尺寸取一次用于核对，别盒、分屏或多显示时直接失败，而不是静默采到别处像素。
            val (windowWidth, windowHeight) = compose.runOnIdle {
                compose.activity.window.decorView.let { it.width to it.height }
            }
            for ((theme, dark, name) in listOf(
                Triple(ThemeChoice.DARK, true, "task-center-heading-dark.png"),
                Triple(ThemeChoice.LIGHT, false, "task-center-heading-light.png"),
            )) {
                settings.savePreferences(baseline.copy(theme = theme))
                compose.waitUntil(10_000) { captureCloudScreenshot(name) { it.headingContrastOk(bounds, dark, windowWidth, windowHeight) } }
            }
        } finally {
            settings.savePreferences(baseline)
        }
        compose.onNodeWithContentDescription("返回").performClick()
        compose.waitForIdle()
        assertEquals("main", model.route.value)
    }

    /** 深色主题要求最亮笔画明显高于均值，浅色主题要求最深笔画明显低于均值；只比较相对亮度。 */
    private fun Bitmap.headingContrastOk(bounds: Rect, dark: Boolean, windowWidth: Int, windowHeight: Int): Boolean {
        // boundsInWindow 只在整屏截图与窗口同尺寸时才是有效坐标；不等就说明前提不成立，硬失败。
        check(width == windowWidth && height == windowHeight) {
            "整屏截图 ${width}x$height 与窗口 ${windowWidth}x$windowHeight 不一致，窗口坐标不能直接用于像素采样"
        }
        val left = bounds.left.toInt().coerceAtLeast(0)
        val right = bounds.right.toInt().coerceAtMost(width - 1)
        val top = bounds.top.toInt().coerceAtLeast(0)
        val bottom = bounds.bottom.toInt().coerceAtMost(height - 1)
        if (right - left < 8 || bottom - top < 8) return false
        var lightest = 0
        var darkest = 255
        var sum = 0L
        var count = 0
        var y = top
        while (y <= bottom) {
            var x = left
            while (x <= right) {
                val pixel = getPixel(x, y)
                val value = (Color.red(pixel) * 299 + Color.green(pixel) * 587 + Color.blue(pixel) * 114) / 1000
                lightest = maxOf(lightest, value)
                darkest = minOf(darkest, value)
                sum += value
                count++
                x += 2
            }
            y += 2
        }
        if (count == 0) return false
        val mean = sum.toDouble() / count
        return if (dark) lightest - mean >= 48 else mean - darkest >= 48
    }

    private fun assertTagSearch(model: LibraryViewModel, graph: DataGraph) {
        compose.onNodeWithText("合成标签").performClick()
        val input = compose.onNodeWithTag("search-input")
        val tagged = awaitSearchResults(model, "合成标签").single()
        input.assertTextEquals("合成标签")
        compose.onNodeWithContentDescription(tagged.name).assertIsDisplayed()
        assertTrue(captureCloudScreenshot("search-from-statistics-tag.png"))
        compose.waitUntil(10_000) { graph.settings.searchHistory.value.firstOrNull() == "合成标签" }
        compose.onNodeWithContentDescription("返回").performClick()
        awaitText("常看作者与标签 · 全部")

        compose.onNode(hasScrollToIndexAction()).performScrollToNode(hasText("常看标签"))
        compose.onNodeWithText("合成标签").performClick()
        awaitSearchResults(model, "合成标签")
        input.performTouchInput { click() }.assertTextEquals("合成标签")
        compose.onNodeWithTag("media-browser:search").assertDoesNotExist()
        input.performTextReplacement("sample_regular_01")
        compose.onNode(hasText("搜索") and hasClickAction()).performClick()
        assertEquals("sample_regular_01.png", awaitSearchResults(model, "sample_regular_01").single().name)
        compose.onNodeWithContentDescription("返回").performClick()
        awaitText("推荐搜索")
        compose.waitUntil(10_000) {
            graph.settings.searchHistory.value.take(2) == listOf("sample_regular_01", "合成标签")
        }
        compose.onNodeWithContentDescription("返回").performClick()
        awaitText("常看作者与标签 · 全部")
    }

    private fun assertManualSearch(model: LibraryViewModel) {
        compose.onNode(hasText("搜索") and hasClickAction()).performClick()
        awaitText("推荐搜索")
        val input = compose.onNodeWithTag("search-input")
        input.performClick().performTextReplacement("合成标签")
        compose.onNode(hasText("搜索") and hasClickAction()).performClick()
        val result = awaitSearchResults(model, "合成标签").single()
        compose.onNodeWithContentDescription(result.name).performClick()
        compose.waitUntil(20_000) { model.asset.value?.id == result.id && model.route.value == "asset:${result.id}" }
        compose.waitUntil(20_000) { compose.onAllNodesWithTag("asset-detail").fetchSemanticsNodes().isNotEmpty() }
        compose.activityRule.scenario.onActivity { it.onBackPressedDispatcher.onBackPressed() }
        input.assertTextEquals("合成标签")
        compose.onNodeWithContentDescription(result.name).assertIsDisplayed()
        input.performTouchInput { click() }.assertTextEquals("合成标签")
        compose.onNodeWithTag("media-browser:search").assertDoesNotExist()
        assertTrue(captureCloudScreenshot("search-edit-with-word.png"))
        compose.onNode(hasText("搜索") and hasClickAction()).performClick()
        awaitSearchResults(model, "合成标签")
        compose.activityRule.scenario.onActivity { it.onBackPressedDispatcher.onBackPressed() }
        awaitText("推荐搜索")
        compose.onNodeWithText("搜索文件、出处、角色、COS作者…").assertIsDisplayed()
        compose.onNodeWithTag("media-browser:search").assertDoesNotExist()
        assertTrue(captureCloudScreenshot("search-back-cleared.png"))
    }

    private fun awaitSearchResults(model: LibraryViewModel, query: String): List<Asset> {
        val state = compose.runOnIdle { model.assets("search") }
        compose.waitUntil(20_000) { state.value.loaded && state.value.loadedQuery?.text == query }
        return state.value.items
    }

    private fun openStatisticsCard(model: LibraryViewModel, label: String, destination: String) {
        val list = compose.onNode(hasScrollToIndexAction())
        list.performScrollToNode(hasText(label))
        val heading = compose.onNodeWithText(label, useUnmergedTree = true).performScrollTo()
        val viewport = list.fetchSemanticsNode().boundsInRoot
        val dockTop = compose.onNodeWithTag("navigation:stats").fetchSemanticsNode().boundsInRoot.top
        val safeBottom = minOf(viewport.bottom, dockTop)
        val delta = heading.fetchSemanticsNode().boundsInRoot.center.y - (viewport.top + safeBottom) / 2f
        list.performSemanticsAction(SemanticsActions.ScrollBy) { it(0f, delta) }
        compose.waitForIdle()
        val target = heading.fetchSemanticsNode().boundsInRoot
        assertTrue("Statistics heading $label must be visible above dock: $target, $viewport, $dockTop",
            target.height > 0f && target.top >= viewport.top && target.bottom < safeBottom)
        // Tap the heading, not the interactive chart or the navigation drawn over the list.
        heading.performTouchInput { click(center) }
        compose.waitForIdle()
        assertEquals("Tap on $label landed on tab ${model.tab.value}", destination, model.route.value)
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
