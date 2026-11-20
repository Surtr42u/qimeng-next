package media.qimeng.next.test

import android.content.res.Configuration
import android.graphics.Bitmap
import android.graphics.Color
import android.view.KeyEvent
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.text.TextLayoutResult
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
import kotlin.math.abs

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

    @Test(timeout = 90_000)
    fun recommendationReadsIndependentlyAndOnlyAcknowledgesAnActualSave() {
        val args = InstrumentationRegistry.getArguments()
        val endpoint = requireNotNull(args.getString("qimengStatisticsUrl"))
        require(endpoint == "http://127.0.0.1:18483/") { "Only the isolated statistics service is allowed" }
        val token = requireNotNull(args.getString("qimengToken"))
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val graph = DataGraph.get(instrumentation.targetContext)
        val previous = graph.settings.session.value
        val previousToken = previous?.let(graph.settings::tokenFor)
        try {
            runBlocking { graph.media.login(endpoint, token) }
            val target = requireNotNull(graph.settings.session.value)
            val original = runBlocking { graph.analytics.preferences(target) }
            try {
                val model = compose.runOnIdle { ViewModelProvider(compose.activity)[LibraryViewModel::class.java] }
                compose.onNodeWithTag("navigation:me").performClick()
                val preferences = model.recommendationPreferences
                // Both inventory and preference reads are launched by the cold Mine tab.
                compose.waitUntil(20_000) { preferences.value.data != null || preferences.value.error != null }
                assertNull(preferences.value.error)
                assertEquals(original, preferences.value.data)

                val entry = hasText("推荐偏好") and hasClickAction()
                val sheet = hasAnyAncestor(hasTestTag("recommendation-settings"))
                val neutral = hasText("点选方案后自动保存") and sheet
                val saved = hasText("已保存到当前服务器") and sheet
                fun await(matcher: SemanticsMatcher) {
                    compose.waitUntil(20_000) { compose.onAllNodes(matcher).fetchSemanticsNodes().isNotEmpty() }
                }
                fun openSheet() {
                    compose.onNodeWithTag("profile-settings-list").performScrollToNode(entry)
                    // Invoke the actual row action without targeting the overlapping floating dock.
                    compose.onNode(entry).performSemanticsAction(SemanticsActions.OnClick) { assertTrue(it()) }
                    await(neutral)
                    compose.onNode(saved).assertDoesNotExist()
                }
                openSheet()
                val familiar = original.presets.getValue("familiar")
                val (label, weights) = if (original.weights == familiar) "新鲜内容" to original.presets.getValue("fresh")
                    else "熟悉内容" to familiar
                val choice = hasText(label) and hasClickAction() and sheet
                compose.onNode(choice).assertIsNotSelected().performClick()
                await(saved)
                compose.onNode(choice).assertIsSelected()
                val actual = runBlocking { graph.analytics.preferences(target) }
                assertEquals(original.revision + 1L, actual.revision)
                assertEquals(weights, actual.weights)

                instrumentation.sendKeyDownUpSync(KeyEvent.KEYCODE_BACK)
                compose.waitUntil(15_000) { compose.onAllNodesWithTag("recommendation-settings").fetchSemanticsNodes().isEmpty() }
                openSheet()
                compose.onNode(choice).assertIsSelected()
                assertEquals(actual, runBlocking { graph.analytics.preferences(target) })
            } finally {
                runBlocking {
                    val current = graph.analytics.preferences(target)
                    if (current.weights != original.weights) graph.analytics.savePreferences(current, original.weights, target)
                }
            }
        } finally {
            if (previous == null) runBlocking { graph.media.disconnect() }
            else graph.settings.saveSession(previous, requireNotNull(previousToken))
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
            assertOverviewColorsAndGeometry(window, report.trends.size, StatsPalette.DARK, "dark")
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
        val sourceChart = hasContentDescription("来源浏览趋势，共 $periods 个时段")
        val checkbox = SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Checkbox)
        val cos = compose.onNode(hasText("COS") and checkbox)
        val regular = compose.onNode(hasText("常规") and checkbox)
        chartList.performScrollToNode(hasText("COS") and checkbox)
        cos.assertIsOn().performClick().assertIsOff()
        regular.assertIsOn().assertIsNotEnabled() // At least one series must remain.
        chartList.performScrollToNode(sourceChart)
        val source = compose.onNode(sourceChart)
        val nextPoint = source.fetchSemanticsNode().config[SemanticsActions.CustomActions].single { it.label == "下一个数据点" }
        compose.runOnIdle { assertTrue(nextPoint.action()) }
        val onlyRegular = source.fetchSemanticsNode().config[SemanticsProperties.StateDescription]
        assertTrue(onlyRegular.contains("常规 "))
        assertFalse(onlyRegular.contains("COS "))
        chartList.performScrollToNode(hasText("COS") and checkbox)
        cos.performClick().assertIsOn()
        assertTrue(source.fetchSemanticsNode().config[SemanticsProperties.StateDescription].contains("COS "))
        chartList.performScrollToIndex(0)
        captureStatisticsPage("stats-source-trends-dark.png", "类型浏览趋势 · 全部")
        compose.onNodeWithContentDescription("返回").performClick()

        openStatisticsCard(model, "分布统计", "stats-detail:distribution:all")
        awaitText("常规 / COS")
        assertMetric("常规 / COS", "51 / 2")
        compose.onNode(hasScrollToIndexAction()).performScrollToNode(hasText("来源分布对比"))
        compose.onNodeWithText("来源分布对比").assertIsDisplayed()
        captureStatisticsPage("stats-partition-distribution-dark.png", "分布统计")
        compose.onNodeWithContentDescription("返回").performClick()

        openStatisticsCard(model, "常看作者与标签", "stats-detail:interests:all")
        awaitText("常看作者与标签 · 全部")
        compose.onNode(hasScrollToIndexAction()).performScrollToNode(hasText("常看标签"))
        compose.onNodeWithText("合成标签").assertIsDisplayed()
        captureStatisticsPage("stats-interests-dark.png", "常看作者与标签 · 全部")
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
        captureStatisticsPage("stats-browsing-review-dark.png", "浏览回顾")
        compose.onNodeWithContentDescription("返回").performClick()
        verifyOtherThemeChoices(model, graph, windows.last(), periods)
        compose.onNodeWithTag("navigation:home").performClick()
        assertEquals(ThemeChoice.DARK, graph.settings.preferences.value.theme)
        compose.waitUntil(10_000) {
            captureCloudScreenshot("stats-return-to-dark-home.png") { screen -> screen.getPixel(4, 32) == Color.rgb(20, 20, 20) }
        }
        assertTaskCenterHeaderContrast(model, graph)
        assertManualSearch(model)
    }

    private fun assertTaskCenterHeaderContrast(model: LibraryViewModel, graph: DataGraph) {
        val settings = graph.settings
        val baseline = settings.preferences.value
        compose.runOnIdle { model.navigate("reports") }
        try {
            val title = compose.onNodeWithText("任务中心", substring = false)
            val bounds = title.assertIsDisplayed().fetchSemanticsNode().boundsInWindow
            // This ordinary route has no Statistics provider or enclosing Surface.
            // Sample the rendered title within its semantics bounds, not a fixed glyph position.
            for ((theme, palette, filename) in listOf(
                Triple(ThemeChoice.DARK, StatsPalette.DARK, "task-center-heading-dark.png"),
                Triple(ThemeChoice.LIGHT, StatsPalette.LIGHT, "task-center-heading-light.png"),
            )) {
                settings.savePreferences(baseline.copy(theme = theme))
                compose.waitUntil(10_000) {
                    captureCloudScreenshot(filename) { screen ->
                        screen.getPixel(4, bounds.center.y.toInt()) == palette.background &&
                            screen.hasColor(bounds, palette.accent)
                    }
                }
            }
        } finally {
            settings.savePreferences(baseline)
        }
        compose.onNodeWithContentDescription("返回").performClick()
        compose.onNodeWithTag("navigation:home").assertIsSelected()
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

    private fun verifyOtherThemeChoices(model: LibraryViewModel, graph: DataGraph, window: Window, periods: Int) {
        val settings = graph.settings
        val baseline = settings.preferences.value
        val list = compose.onNode(hasScrollToIndexAction())
        list.performScrollToIndex(0)
        try {
            settings.savePreferences(baseline.copy(theme = ThemeChoice.LIGHT))
            assertOverviewColorsAndGeometry(window, periods, StatsPalette.LIGHT, "light")
            openStatisticsCard(model, "分类型趋势 ›", "stats-detail:trends:all")
            awaitText("类型浏览趋势 · 全部")
            captureStatisticsPage("stats-source-trends-light.png", "类型浏览趋势 · 全部", StatsPalette.LIGHT)
            compose.onNodeWithContentDescription("返回").performClick()
            list.performScrollToIndex(0)

            val systemDark = compose.runOnIdle {
                compose.activity.resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK == Configuration.UI_MODE_NIGHT_YES
            }
            settings.savePreferences(baseline.copy(theme = ThemeChoice.SYSTEM))
            assertOverviewColorsAndGeometry(window, periods,
                if (systemDark) StatsPalette.DARK else StatsPalette.LIGHT, "system")
        } finally {
            settings.savePreferences(baseline)
        }
    }

    private fun assertOverviewColorsAndGeometry(window: Window, periods: Int, palette: StatsPalette, evidence: String) {
        val labels = listOf("总浏览次数", "总播放次数", "总浏览时长", "总文件数", "总占用空间", "平均浏览次数")
        val bounds = labels.map { compose.onNodeWithText(it, useUnmergedTree = true).fetchSemanticsNode().boundsInWindow }
        val rows = bounds.chunked(3)
        assertTrue(rows[0].maxOf { it.bottom } < rows[1].minOf { it.top })
        rows.forEach { row -> assertTrue(row.zipWithNext().all { (left, right) -> left.right < right.left }) }

        val first = bounds.first()
        val chart = compose.onNodeWithContentDescription("浏览趋势，共 $periods 个时段")
            .assertIsDisplayed().fetchSemanticsNode().boundsInWindow
        val dates = Rect(chart.left, chart.bottom - chart.height / 8, chart.right, chart.bottom)
        val selected = compose.onNodeWithText(window.label).assertIsSelected().fetchSemanticsNode().boundsInWindow
        val otherLabel = if (window.label == "7天") "30天" else "7天"
        val other = compose.onNodeWithText(otherLabel).assertIsNotSelected().fetchSemanticsNode().boundsInWindow
        val selectedInk = assertSegmentTextColor(window.label, palette.selectedText)
        val otherInk = assertSegmentTextColor(otherLabel, palette.secondaryText)
        var missing = emptyList<String>()
        try {
            compose.waitUntil(10_000) {
                captureCloudScreenshot("stats-overview-$evidence-" + window.range + ".png") { screen ->
                    missing = buildList {
                        if (screen.getPixel(4, first.center.y.toInt()) != palette.background) add("page background")
                        if (screen.getPixel(first.center.x.toInt(), first.bottom.toInt() + 3) != palette.card) add("metric card")
                        if (screen.pillAt(selected) != palette.accent) add("selected capsule")
                        if (screen.pillAt(other) != palette.track) add("unselected track")
                        if (!screen.hasGlyphInk(selectedInk, palette.selectedText, palette.accent)) add("selected label")
                        if (!screen.hasGlyphInk(otherInk, palette.secondaryText, palette.track)) add("unselected label")
                        if (!screen.hasColor(chart, palette.accent)) add("chart line")
                        if (!screen.hasColor(chart, palette.plotFill)) add("chart fill")
                        if (!screen.hasColor(dates, palette.secondaryText)) add("date axis")
                    }
                    missing.isEmpty()
                }
            }
        } catch (timeout: ComposeTimeoutException) {
            throw AssertionError("Statistics $evidence/${window.range} did not render: ${missing.joinToString()}", timeout)
        }
    }

    private fun assertSegmentTextColor(label: String, expected: Int): Rect {
        val text = compose.onNodeWithText(label, substring = false, useUnmergedTree = true).assertIsDisplayed()
        var actual: Int? = null
        try {
            compose.waitUntil(10_000) {
                val layouts = mutableListOf<TextLayoutResult>()
                text.performSemanticsAction(SemanticsActions.GetTextLayoutResult) { it(layouts) }
                actual = layouts.singleOrNull()?.layoutInput?.style?.color?.toArgb()
                actual == expected
            }
        } catch (timeout: ComposeTimeoutException) {
            throw AssertionError("Resolved text color for $label: expected $expected, got $actual", timeout)
        }
        return text.fetchSemanticsNode().boundsInWindow
    }

    private fun captureStatisticsPage(name: String, label: String, palette: StatsPalette = StatsPalette.DARK) {
        val y = compose.onNodeWithText(label).fetchSemanticsNode().boundsInWindow.center.y.toInt()
        compose.waitUntil(10_000) {
            captureCloudScreenshot(name) { screen -> screen.getPixel(4, y) == palette.background }
        }
    }

    // Take solid pill samples well left of the center-aligned label, inside the rounded ends.
    private fun Bitmap.pillAt(bounds: Rect) =
        getPixel((bounds.left + bounds.width / 5).toInt(), bounds.center.y.toInt())

    private fun Bitmap.hasGlyphInk(bounds: Rect, ink: Int, backdrop: Int): Boolean {
        // Tiny antialiased glyphs need not contain eight nearly solid pixels. Check for ink
        // within one eighth of the ink/background color distance, inside the actual text bounds.
        val tolerance = maxOf(abs(Color.red(ink) - Color.red(backdrop)),
            abs(Color.green(ink) - Color.green(backdrop)), abs(Color.blue(ink) - Color.blue(backdrop))) / 8
        return hasColor(bounds, ink, tolerance)
    }

    private fun Bitmap.hasColor(bounds: Rect, color: Int, tolerance: Int = 7): Boolean {
        val left = bounds.left.toInt().coerceIn(0, width)
        val right = bounds.right.toInt().coerceIn(left, width)
        val top = bounds.top.toInt().coerceIn(0, height)
        val bottom = bounds.bottom.toInt().coerceIn(top, height)
        var hits = 0
        for (y in top until bottom) for (x in left until right) {
            val pixel = getPixel(x, y)
            // Text and thin chart strokes include antialiasing, unlike the solid surfaces above.
            if (abs(Color.red(pixel) - Color.red(color)) <= tolerance &&
                abs(Color.green(pixel) - Color.green(color)) <= tolerance &&
                abs(Color.blue(pixel) - Color.blue(color)) <= tolerance && ++hits >= 8) return true
        }
        return false
    }

    private enum class StatsPalette(
        val background: Int, val card: Int, val track: Int, val accent: Int,
        val selectedText: Int, val secondaryText: Int, val plotFill: Int,
    ) {
        DARK(0xff141414.toInt(), 0xff2e2e2e.toInt(), 0xff2e2e2e.toInt(), 0xffc8c8c8.toInt(),
            0xff141414.toInt(), 0xffa3a3a3.toInt(), 0xff464646.toInt()),
        LIGHT(0xfff1f2f4.toInt(), Color.WHITE, 0xffeaebef.toInt(), 0xff3a3a3a.toInt(),
            Color.WHITE, 0xff666666.toInt(), 0xffe0e0e0.toInt()),
    }

    private data class Window(val range: String, val label: String, val views: Long, val seconds: Long)
}
