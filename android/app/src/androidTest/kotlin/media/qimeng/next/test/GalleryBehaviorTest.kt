package media.qimeng.next.test

import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.lifecycle.ViewModelProvider
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.runBlocking
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.MainActivity
import media.qimeng.next.data.DataGraph
import media.qimeng.next.data.SecureSettings
import media.qimeng.next.model.Asset
import media.qimeng.next.testsupport.captureCloudScreenshot
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test

class GalleryBehaviorTest {
    @get:Rule val compose = createAndroidComposeRule<MainActivity>()

    @Test(timeout = 180_000)
    fun longPressBatchAppliesEachRevisionAndRetriesOnlyTheConflict() = isolatedBrowse { graph, model ->
        graph.settings.saveAlbumColumns(3)
        val catalog = openAlbum(model)
        val first = catalog.getValue("album-first.png")
        val second = catalog.getValue("album-second.png")
        val cos = catalog.getValue("album-cos.png")
        val replacement = runBlocking { graph.media.authors(query = "合成替换作者", kind = "regular").items.single() }
        longPress(first)
        tile(first).assertIsSelected()
        compose.onNodeWithText("全选").performClick()
        compose.onNodeWithText("已选 50 项").assertIsDisplayed()
        compose.onNodeWithText("清空").performClick()
        compose.onNodeWithText("已选 0 项").assertIsDisplayed()
        compose.onNodeWithText("退出").performClick()
        tile(first).assert(hasClickAction())

        longPress(first)
        tile(second).performClick().assertIsSelected()
        tile(cos).performClick().assertIsSelected()
        compose.onNodeWithText("已选 3 项").assertIsDisplayed()
        assertTrue(captureCloudScreenshot("album-multiple-selection.png"))
        val editedElsewhere = runBlocking { graph.media.updateNote(second, "并发保留备注") }
        openBatch(3)
        compose.onNodeWithTag("batch-author-input").performTextReplacement("合成替换作者")
        awaitText("合成替换作者")
        compose.onNodeWithText("合成替换作者").performClick()
        compose.onNodeWithText("替换 3 项作者").performClick()
        awaitText("成功 2 项 · 失败 1 项")
        compose.onNodeWithText(first.relativePath).assertIsDisplayed()
        compose.onNodeWithText(second.relativePath).assertIsDisplayed()
        assertTrue(captureCloudScreenshot("album-batch-conflict.png"))

        val firstSaved = read(graph, first)
        val cosSaved = read(graph, cos)
        val secondPending = read(graph, second)
        assertEquals(listOf(replacement.id), firstSaved.authorIds)
        assertEquals(cos.authorIds.toSet() + replacement.id, cosSaved.authorIds.toSet())
        assertEquals(editedElsewhere.revision, secondPending.revision)
        assertEquals(second.authorIds, secondPending.authorIds)
        assertUnchangedMedia(first, firstSaved)
        assertUnchangedMedia(cos, cosSaved)

        compose.onNodeWithText("核对冲突项").performClick()
        awaitText("确认重试 1 项")
        assertEquals(editedElsewhere.revision, read(graph, second).revision)
        compose.onNodeWithText("确认重试 1 项").performClick()
        awaitText("成功 3 项 · 失败 0 项")
        assertEquals(firstSaved.revision, read(graph, first).revision)
        assertEquals(cosSaved.revision, read(graph, cos).revision)
        val secondSaved = read(graph, second)
        assertEquals(listOf(replacement.id), secondSaved.authorIds)
        assertUnchangedMedia(editedElsewhere, secondSaved)
        compose.onNodeWithText("完成").performClick()

        val reloaded = albumEntries(model)
        longPress(reloaded.getValue(first.name))
        tile(reloaded.getValue(second.name)).performClick()
        openBatch(2)
        compose.onNodeWithText("获取来源").performClick()
        compose.onNodeWithTag("batch-acquisition-input").performTextReplacement("新增合成渠道\n新增合成渠道")
        compose.onNodeWithText("追加到 2 项").performClick()
        awaitText("成功 2 项 · 失败 0 项")
        val firstSources = read(graph, first)
        val secondSources = read(graph, second)
        assertEquals(firstSaved.acquisitionSources + "新增合成渠道", firstSources.acquisitionSources)
        assertEquals(secondSaved.acquisitionSources + "新增合成渠道", secondSources.acquisitionSources)
        assertUnchangedMedia(firstSaved, firstSources.copy(acquisitionSources = firstSaved.acquisitionSources))
        assertUnchangedMedia(secondSaved, secondSources.copy(acquisitionSources = secondSaved.acquisitionSources))
        assertEquals(firstSaved.authorIds, firstSources.authorIds)
        assertEquals(secondSaved.authorIds, secondSources.authorIds)
        compose.onNodeWithText("完成").performClick()

        longPress(albumEntries(model).getValue(first.name))
        openBatch(1)
        val args = InstrumentationRegistry.getArguments()
        val other = requireNotNull(args.getString("qimengSourceUrl"))
        require(other == "http://127.0.0.1:18482/")
        runBlocking { graph.media.login(other, requireNotNull(args.getString("qimengSourceToken"))) }
        compose.waitUntil(20_000) {
            compose.onAllNodesWithText("批量设置作者与来源").fetchSemanticsNodes().isEmpty() && model.session.value?.baseUrl == other
        }
        compose.onNodeWithText("已选 1 项").assertDoesNotExist()
    }

    @Test(timeout = 150_000)
    fun dateBucketsAndSharedGridSettingsSurvivePagingAndReopening() = isolatedBrowse { graph, model ->
        graph.settings.saveAlbumColumns(2)
        openAlbum(model)
        val toggled = 3
        compose.onNodeWithContentDescription("切换列数").performClick()
        awaitColumns("albums", toggled)

        openProfileGallery("收藏")
        val favorites = awaitPage(model, "favorites", 50)
        assertEquals("favorited", compose.runOnIdle { model.query("favorites").sort })
        assertEquals(53L, favorites.value.totalMatched)
        assertNotNull(favorites.value.cursor)
        awaitColumns("favorites", toggled)
        compose.onNodeWithText("今天  51 项").assertIsDisplayed()
        assertTrue(captureCloudScreenshot("favorites-first-page-count.png"))
        grid("favorites").performScrollToIndex(48)
        compose.waitUntil(20_000) { favorites.value.items.size == 53 && favorites.value.cursor == null }
        grid("favorites").performScrollToNode(hasText("未知日期  1 项"))
        compose.onNodeWithText("未知日期  1 项").assertIsDisplayed()
        assertNull(favorites.value.items.last().favoritedAt)
        assertEquals("album-cos.png", favorites.value.items.last().name)
        val oldFavorite = favorites.value.items.single { it.name == "album-first.png" }
        assertTrue(oldFavorite.fileDate.orEmpty().startsWith("2002-"))
        assertNotNull(oldFavorite.favoritedAt)
        grid("favorites").performScrollToNode(hasText("2024-03-04  1 项"))
        compose.onNodeWithText("2024-03-04  1 项").assertIsDisplayed()
        grid("favorites").performScrollToIndex(0)
        compose.onNodeWithText("今天  51 项").assertIsDisplayed()
        val next = 4
        grid("favorites").performTouchInput {
            val x = center.x
            val y = center.y
            pinch(Offset(x - width * 0.24f, y), Offset(x - width * 0.18f, y),
                Offset(x + width * 0.24f, y), Offset(x + width * 0.18f, y))
        }
        compose.waitUntil(15_000) { graph.settings.albumColumns.value == next }
        awaitColumns("favorites", next)
        compose.onNodeWithContentDescription("返回").performClick()
        openProfileGallery("浏览历史")
        awaitPage(model, "history", 2)
        awaitColumns("history", next)
        compose.onNodeWithText("今天  1 项").assertIsDisplayed()
        compose.onNodeWithText("2024-03-04  1 项").assertIsDisplayed()
        assertTrue(captureCloudScreenshot("history-latest-view-grid.png"))

        compose.activityRule.scenario.recreate()
        awaitColumns("history", next)
        val reopened = SecureSettings(InstrumentationRegistry.getInstrumentation().targetContext)
        assertEquals(next, reopened.albumColumns.value)
        compose.onNodeWithContentDescription("返回").performClick()
        compose.onNodeWithTag("navigation:albums").performClick()
        awaitColumns("albums", next)
    }

    private fun isolatedBrowse(body: (DataGraph, LibraryViewModel) -> Unit) {
        val args = InstrumentationRegistry.getArguments()
        val endpoint = requireNotNull(args.getString("qimengBrowseUrl"))
        require(endpoint == "http://127.0.0.1:18485/") { "Only the isolated browse service is allowed" }
        val graph = DataGraph.get(InstrumentationRegistry.getInstrumentation().targetContext)
        val settings = graph.settings
        val previous = settings.session.value
        val previousToken = previous?.let(settings::tokenFor)
        val previousColumns = settings.albumColumns.value
        try {
            runBlocking { graph.media.login(endpoint, requireNotNull(args.getString("qimengToken"))) }
            val model = compose.runOnIdle { ViewModelProvider(compose.activity)[LibraryViewModel::class.java] }
            compose.runOnIdle { model.selectTab(0) }
            body(graph, model)
        } finally {
            settings.saveAlbumColumns(previousColumns)
            if (previous == null) runBlocking { graph.media.disconnect() } else settings.saveSession(previous, requireNotNull(previousToken))
        }
    }

    private fun openAlbum(model: LibraryViewModel): Map<String, Asset> {
        compose.onNodeWithTag("navigation:albums").performClick()
        return albumEntries(model)
    }

    private fun albumEntries(model: LibraryViewModel): Map<String, Asset> =
        awaitPage(model, "albums", 50).value.items.associateBy(Asset::name)

    private fun awaitPage(model: LibraryViewModel, key: String, size: Int) = compose.runOnIdle { model.assets(key) }.also { state ->
        compose.waitUntil(25_000) { state.value.loaded && state.value.items.size == size }
    }

    private fun openProfileGallery(label: String) {
        compose.onNodeWithTag("navigation:me").performClick()
        compose.onNodeWithTag("profile-settings-list").performScrollToNode(hasText(label))
        compose.onNodeWithText(label).performClick()
    }

    private fun awaitColumns(key: String, columns: Int) {
        val configured = hasTestTag("gallery-grid:$key") and SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "$columns 列")
        compose.waitUntil(15_000) { compose.onAllNodes(configured, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }
    }

    private fun grid(key: String) = compose.onNodeWithTag("gallery-grid:$key", useUnmergedTree = true)
    private fun tile(asset: Asset) = compose.onNodeWithContentDescription(asset.name)
    private fun longPress(asset: Asset) = tile(asset).performTouchInput { longClick() }
    private fun openBatch(count: Int) = compose.onNodeWithText("批量设置作者与来源 ($count)").performClick()

    private fun awaitText(text: String) {
        compose.waitUntil(20_000) { compose.onAllNodesWithText(text).fetchSemanticsNodes().isNotEmpty() }
    }

    private fun read(graph: DataGraph, asset: Asset) = runBlocking { graph.media.asset(asset.id, asset.target) }

    private fun assertUnchangedMedia(before: Asset, after: Asset) {
        assertEquals(before.id, after.id)
        assertEquals(before.version, after.version)
        assertEquals(before.sourceIds, after.sourceIds)
        assertEquals(before.tags, after.tags)
        assertEquals(before.note, after.note)
        assertEquals(before.acquisitionSources, after.acquisitionSources)
    }
}
