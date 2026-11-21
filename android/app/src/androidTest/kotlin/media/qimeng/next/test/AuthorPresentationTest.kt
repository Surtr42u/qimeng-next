package media.qimeng.next.test

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.net.Uri
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModelProvider
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.MainActivity
import media.qimeng.next.AuthorProfileViewModel
import media.qimeng.next.data.DataGraph
import media.qimeng.next.model.*
import media.qimeng.next.testsupport.captureCloudScreenshot
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import java.util.UUID

/** Production routes and the SDK against the isolated same-SHA server, with synthetic records only. */
@OptIn(ExperimentalTestApi::class)
class AuthorPresentationTest {
    @get:Rule val compose = createAndroidComposeRule<MainActivity>()

    private val original = "Synthetic creator.\nOriginal line two.\nOriginal line three.\nOriginal line four.\nOriginal line five."
    private val chinese = "合成译文第一行。\n第二行。\n第三行。\n第四行。\n第五行依然完整。"
    private val summary = "作者整理简介预览。\n\n完整整理简介和核验说明。"
    private val proposed = "待核验的合成作者简介。"
    private val assetCount = 12
    private val card = hasTestTag("author-profile-card")
    private val count = hasTestTag("author-result-count")
    private val filters = hasTestTag("author-filter-dimensions")
    private val media = hasContentDescription("作者界面作品-", substring = true)
    private val grid get() = compose.onNodeWithTag("author-media-scroll")
    private val overview get() = compose.onNodeWithTag("author-profile-overview")
    private val footer get() = compose.onNodeWithTag("author-profile-save-bar")
    private var width = 0

    @Test(timeout = 360_000)
    fun authorLayoutEditorAndCandidateRoundTrip() {
        val args = InstrumentationRegistry.getArguments()
        val server = requireNotNull(args.getString("qimengServerUrl"))
        require(server == "http://127.0.0.1:18481/") { "An isolated cloud server is required" }
        width = requireNotNull(args.getString("qimengAuthorWidth")).toInt()
        require(width in listOf(320, 360))
        val font = requireNotNull(args.getString("qimengAuthorFont")).toFloat()
        compose.activityRule.scenario.onActivity {
            assertEquals(width, it.resources.configuration.screenWidthDp)
            assertEquals(font, it.resources.configuration.fontScale, 0.03f)
        }
        val graph = DataGraph.get(InstrumentationRegistry.getInstrumentation().targetContext)
        val suffix = UUID.randomUUID().toString().take(8)
        val author = runBlocking {
            graph.awaitRecovery()
            graph.media.login(server, requireNotNull(args.getString("qimengToken")))
            seed(graph, suffix)
        }
        try {
            openDirectory(suffix)
            compose.onNodeWithText(author.name).performClick()
            val loading = hasTestTag("author-media-loading")
            // The header count is seeded from the tapped author, so the list can
            // finish before this frame: assert that a first row follows the
            // filters, not that a specific loading frame was captured.
            val firstRow = loading or media or hasText("没有符合条件的媒体")
            await(hasTestTag("author-media-scroll"))
            await(firstRow)
            assertOrder(firstRow)
            screenshot("first-row")

            await(hasText("本地作品 · 筛选结果 $assetCount 个文件"))
            // The count is displayed from the tap seed before the list loads, so
            // wait for the assets themselves before asserting their order.
            compose.waitUntil(30_000) { compose.onAllNodes(media).fetchSemanticsNodes().isNotEmpty() }
            grid.performScrollToIndex(0)
            assertOrder(media)
            assertFullBiography()
            val follow = hasText("关注") and hasAnyAncestor(card)
            val inventory = hasText("$assetCount 个本地文件") and hasAnyAncestor(card)
            assertDisjoint(hasTestTag("author-profile-name"), follow)
            assertDisjoint(inventory, follow)
            screenshot("local")

            compose.onNode(follow).performClick()
            await(hasText("处理中…"))
            compose.onNodeWithText("处理中…").assertIsNotEnabled()
            await(hasText("重试关注"))
            compose.onNodeWithText("重试关注").performClick()
            await(hasText("已关注") and hasAnyAncestor(card))
            assertTrue(runBlocking { graph.media.author(author.id).followed })

            collapseAndFilter()
            val newWorks = hasText("作者新作") and hasClickAction()
            grid.performScrollToIndex(0)
            compose.onNode(newWorks).performClick()
            await(hasText("尚未接入可用的作品发布订阅", substring = true))
            screenshot("new-works")
            compose.onNode(hasText("本地作品") and hasClickAction()).performClick()
            await(count)

            grid.performScrollToIndex(0)
            grid.performTouchInput { swipeUp(startY = centerY + 45f, endY = centerY - 45f, durationMillis = 500) }
            val details = compose.onNodeWithText("详细资料").assertIsDisplayed()
            val detailsTop = details.getUnclippedBoundsInRoot().top
            details.performClick()
            compose.onNodeWithTag("author-profile-top-bar").assertIsDisplayed()
            await(hasText("身份已确认"))
            verifyOverview(author)
            verifyEditor(graph, author)
            verifyCandidate(graph, author)
            compose.onNode(hasContentDescription("返回") and hasAnyAncestor(hasTestTag("author-profile-top-bar"))).performClick()
            await(hasText("详细资料"))
            val restoredTop = compose.onNodeWithText("详细资料").getUnclippedBoundsInRoot().top
            assertEquals("Author grid restores its position after profile maintenance", detailsTop.value, restoredTop.value, 2f)
            compose.onNodeWithText("原文").assertIsSelected()

            compose.onNodeWithContentDescription("返回").performClick()
            verifyDirectorySources(graph, author)
        } catch (failure: Throwable) {
            screenshot("failure")
            throw failure
        }
    }

    private suspend fun seed(graph: DataGraph, suffix: String): Author {
        val target = requireNotNull(graph.settings.session.value)
        graph.authorLists.applyDraft(AuthorImportDraft(target, "author-ui-$suffix.txt",
            "1  合成双名作者${suffix.take(4)}  Synthetic Art Studio $suffix\n作品\n未匹配合成声明-$suffix.png\n", 0, key()))
        val author = graph.media.authors(query = suffix).items.single()
        assertNotNull(author.txtOrder)
        graph.authorProfiles.profile(author.id).let { profile ->
            graph.authorProfiles.save(profile, profile.blocks.single { it.block == "x" },
                ProfileValues(profileUrl = "https://x.com/synthetic_ui", biographyOriginal = original, biographyZh = chinese), emptyList(), null, true)
            graph.authorProfiles.save(profile, profile.blocks.single { it.block == "basic" }, ProfileValues(summary = summary), emptyList(), null, false)
            graph.authorProfiles.save(profile, profile.blocks.single { it.block == "accounts" },
                ProfileValues(accounts = listOf(ProfileAccount("合成发布账号", "artist-$suffix", "https://example.org/author", "https://example.org/works")),
                    identityStatus = "verified"), emptyList(), null, false)
        }
        seedCandidate(graph, target, author.id)
        val library = graph.media.createLibrary("作者 UI 合成作品 $suffix", false)
        val batch = key()
        val uri = Uri.parse("content://media.qimeng.next.test.fixture/android-synthetic.png")
        val resolver = InstrumentationRegistry.getInstrumentation().targetContext.contentResolver
        val red = if (width == 320) 83 else 38
        val batchPixels = batch.replace("-", "")
        val inputs = (1..assetCount).map { index ->
            val green = 63 + index * 8
            val bitmap = Bitmap.createBitmap(96, 64, Bitmap.Config.ARGB_8888)
            try {
                Canvas(bitmap).apply {
                    drawColor(Color.rgb(red, green, 184))
                    drawCircle(46f, 32f, index + 10f, Paint().apply { color = Color.rgb(249, 180, 89) })
                }
                // The Go server identifies originals by bytes, not by the test's unique names.
                batchPixels.forEachIndexed { x, hex ->
                    bitmap.setPixel(x, 0, Color.rgb(red, green, 176 + hex.digitToInt(16)))
                }
                requireNotNull(resolver.openOutputStream(uri)).use { check(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
            } finally { bitmap.recycle() }
            graph.uploads.receive(listOf(uri)).single()
        }
        val stems = inputs.mapIndexed { index, id -> id to "作者界面作品-${index + 1}-$suffix" }.toMap()
        graph.uploads.enqueue(library.id, "", UploadMode.ORDINARY, UploadOptions(authorIds = listOf(author.id)), target,
            inputIds = inputs, editedStems = stems, batchKey = batch)
        withTimeout(120_000) {
            while (true) {
                val transfers = graph.queue.state.value?.transfers.orEmpty().filter { it.batchKey == batch }
                val rejected = transfers.firstOrNull { it.state == TransferState.FAILED }
                check(rejected == null) { "Synthetic author media failed to publish: ${rejected?.message}" }
                if (transfers.size == assetCount && transfers.all { it.state == TransferState.SUCCEEDED }) break
                delay(500)
            }
        }
        val published = graph.media.author(author.id)
        assertEquals(assetCount.toLong(), published.fileCount)
        return published
    }

    private suspend fun seedCandidate(graph: DataGraph, target: Session, authorId: String) = withContext(Dispatchers.IO) {
        // The app only consumes candidates. Set up this producer-side fixture through the published Go contract.
        val evidence = JSONObject().put("url", "https://example.org/author").put("title", "合成核验依据")
            .put("fields", JSONArray().put("summary")).put("status", "candidate").put("note", "云端合成测试")
        val revision = graph.authorProfiles.profile(authorId).blocks.single { it.block == "basic" }.revision
        val body = JSONObject().put("block", "basic").put("baseRevision", revision)
            .put("values", JSONObject().put("summary", proposed)).put("evidence", JSONArray().put(evidence))
        val request = Request.Builder().url("${target.baseUrl}api/v1/authors/$authorId/profile/candidates")
            .header("Idempotency-Key", key()).post(body.toString().toRequestBody("application/json".toMediaType())).build()
        graph.media.mediaClient(target).newCall(request).execute().use { assertEquals(201, it.code) }
    }

    private fun openDirectory(search: String) {
        compose.activityRule.scenario.onActivity { ViewModelProvider(it)[LibraryViewModel::class.java].navigate("authors") }
        await(hasTestTag("author-search"))
        compose.onNodeWithTag("author-search").performTextReplacement(search)
        compose.waitUntil(30_000) {
            compose.onAllNodesWithContentDescription("的作者操作", substring = true).fetchSemanticsNodes().size == 1
        }
    }

    private fun assertOrder(last: SemanticsMatcher) {
        grid.performScrollToIndex(0)
        val cardBounds = compose.onNode(card).getUnclippedBoundsInRoot()
        val countBounds = compose.onNode(count).getUnclippedBoundsInRoot()
        val filterBounds = compose.onNode(filters).getUnclippedBoundsInRoot()
        assertTrue("Card bottom $cardBounds must precede count $countBounds", cardBounds.bottom < countBounds.top)
        assertTrue("Count $countBounds must precede filters $filterBounds", countBounds.bottom < filterBounds.top)

        // At 320dp with enlarged type the full biography legitimately fills the viewport.
        // Bring the filters and first media row into one frame, then measure both anew.
        if (compose.onAllNodes(last).fetchSemanticsNodes().isEmpty()) grid.performScrollToKey("author-filters")
        val adjacentFilters = compose.onNode(filters).getUnclippedBoundsInRoot()
        val cells = compose.onAllNodes(last)
        val indices = cells.fetchSemanticsNodes().indices
        assertFalse("No first loading, empty, or media row below the author filters", indices.isEmpty())
        val firstTop = indices.minOf { cells[it].getUnclippedBoundsInRoot().top }
        assertTrue("Filter row $adjacentFilters must precede the first item $firstTop", adjacentFilters.bottom < firstTop)
        grid.performScrollToIndex(0)
    }

    private fun assertDisjoint(first: SemanticsMatcher, second: SemanticsMatcher) {
        val a = compose.onNode(first).getUnclippedBoundsInRoot()
        val b = compose.onNode(second).getUnclippedBoundsInRoot()
        assertTrue("Author identity, count and action should have separate bounds: $a, $b",
            a.right <= b.left || b.right <= a.left || a.bottom <= b.top || b.bottom <= a.top)
    }

    private fun assertFullBiography() {
        val result = mutableListOf<TextLayoutResult>()
        compose.onNodeWithText(chinese).performSemanticsAction(SemanticsActions.GetTextLayoutResult) { it(result) }
        val text = result.single()
        assertTrue("All five biography lines must participate in layout", text.lineCount >= chinese.lines().size)
        assertFalse("The biography must not be ellipsized", (0 until text.lineCount).any(text::isLineEllipsized))
        assertEquals("The last displayed line includes the end of the biography", chinese.length,
            text.getLineEnd(text.lineCount - 1, visibleEnd = true))
        compose.onNodeWithText("中文").assertIsSelected()
        compose.onNodeWithText("原文").performClick()
        compose.onNodeWithText(original).assertIsDisplayed()
    }

    private fun collapseAndFilter() {
        val type = hasAnyAncestor(filters) and hasText("类型", substring = true)
        val video = hasText("视频 (0)")
        compose.onNode(type).performScrollTo().performClick()
        grid.performScrollToNode(video)
        compose.onNode(video).assertIsDisplayed()
        screenshot("expanded")
        compose.onNode(video).performClick()
        // The confirmed zero result belongs to the selected query; the empty row is a separate lazy item.
        await(hasText("本地作品 · 筛选结果 0 个文件"))
        val empty = hasText("没有符合条件的媒体")
        assertOrder(empty)
        grid.performScrollToNode(empty)
        compose.onNode(empty).assertIsDisplayed()
        screenshot("empty")
        val all = hasText("全部 ($assetCount)")
        grid.performScrollToNode(all)
        compose.onNode(all).assertIsDisplayed().performClick()
        await(hasText("本地作品 · 筛选结果 $assetCount 个文件"))
        grid.performScrollToNode(type)
        compose.onNode(type).performClick()
        compose.onNode(video).assertDoesNotExist()
        compose.onNode(hasAnyAncestor(filters) and hasText("全部 ▼")).assertIsDisplayed()
    }

    private fun verifyOverview(author: Author) {
        val translation = hasText("查看中文译文")
        overview.performScrollToNode(translation)
        compose.onNode(translation).performClick()
        overview.performScrollToNode(hasText(chinese))
        compose.onNodeWithText(chinese).assertIsDisplayed()
        screenshot("overview-x")
        overview.performScrollToNode(hasText("收起中文译文"))
        compose.onNodeWithText("收起中文译文").performClick()
        overview.performScrollToNode(hasText("作者清单第 ${author.txtOrder} 位"))
        compose.onNodeWithText("入库记录 ", substring = true).assertIsDisplayed()
        val complete = hasText("查看完整说明")
        overview.performScrollToNode(complete)
        compose.onNodeWithText(summary).assertDoesNotExist()
        compose.onNode(complete).performClick()
        overview.performScrollToNode(hasText(summary))
        compose.onNodeWithText(summary).assertIsDisplayed()
        overview.performScrollToNode(hasText("合成发布账号", substring = true))
        overview.performScrollToNode(hasText("编辑作品订阅"))
        compose.onNodeWithTag("author-profile-top-bar").assertIsDisplayed()
    }

    private fun verifyEditor(graph: DataGraph, author: Author) {
        val edit = hasText("编辑X 资料")
        overview.performScrollToNode(edit)
        compose.onNode(edit).performClick()
        compose.onNodeWithTag("author-profile-editor-top-bar").assertIsDisplayed()
        compose.onNodeWithText("保存资料").assertIsNotEnabled()
        val baseline = footer.getUnclippedBoundsInRoot().bottom
        val biography = compose.onNodeWithTag("author-biography-edit")
        biography.performScrollTo().performClick()
        compose.waitUntil(20_000) { footer.getUnclippedBoundsInRoot().bottom < baseline - 80.dp }
        biography.performTextReplacement("变更后的合成原文")
        compose.onNodeWithText("保存资料").assertIsDisplayed().assertIsEnabled()
        screenshot("editor-keyboard")
        compose.onNodeWithText("中文").performScrollTo().performClick()
        compose.onNodeWithText("中文译文").assertExists()
        compose.onNodeWithText(chinese).assertDoesNotExist()
        compose.onNodeWithText("原文").performClick()
        biography.assertTextContains("变更后的合成原文")

        val concurrentName = "另一端更新的合成姓名"
        val concurrentRevision = runBlocking {
            val document = graph.authorProfiles.profile(author.id)
            val block = document.blocks.single { it.block == "x" }
            graph.authorProfiles.save(document, block, ProfileValues(displayName = concurrentName), block.evidence, null, false).revision
        }
        compose.onNodeWithText("保存资料").performClick()
        val conflict = hasText("服务器状态已变化或存在重复，请刷新核对后再决定") and
            hasAnyAncestor(hasTestTag("author-profile-save-bar"))
        await(conflict)
        compose.onNode(conflict).assertIsDisplayed()
        biography.assertTextContains("变更后的合成原文")
        val stored = runBlocking { graph.authorProfiles.profile(author.id).blocks.single { it.block == "x" } }
        assertEquals(concurrentRevision, stored.revision)
        assertEquals(concurrentName, stored.values.displayName)
        assertEquals(original, stored.values.biographyOriginal)
        screenshot("editor-conflict")

        compose.onNodeWithText("取消").performClick()
        await(hasText("放弃未保存的修改？"))
        compose.onNodeWithText("继续编辑").performClick()
        biography.assertTextContains("变更后的合成原文")
        compose.onNodeWithText("取消").performClick()
        compose.onNodeWithText("放弃修改").performClick()
        compose.onNodeWithTag("author-profile-editor-body").assertDoesNotExist()
    }

    private fun verifyCandidate(graph: DataGraph, author: Author) {
        lateinit var model: AuthorProfileViewModel
        compose.activityRule.scenario.onActivity { model = ViewModelProvider(it)[AuthorProfileViewModel::class.java] }
        val candidates = hasText("查看候选资料")
        overview.performScrollToNode(candidates)
        compose.onNode(candidates).performClick()
        val review = hasText("载入候选草稿")
        compose.waitUntil(30_000) { model.candidates.value.loaded }
        overview.performScrollToNode(review)
        compose.onNode(review).performClick()
        compose.onNodeWithTag("author-profile-editor-top-bar").assertIsDisplayed()
        assertEquals(summary, runBlocking { graph.authorProfiles.profile(author.id).blocks.single { it.block == "basic" }.values.summary })
        compose.onNodeWithText("保存资料").assertIsEnabled().performClick()
        await(hasTestTag("author-profile-overview"))
        compose.waitUntil(30_000) { compose.onAllNodesWithTag("author-profile-editor-top-bar").fetchSemanticsNodes().isEmpty() }
        assertEquals(proposed, runBlocking { graph.authorProfiles.profile(author.id).blocks.single { it.block == "basic" }.values.summary })
        assertEquals("applied", runBlocking { graph.authorProfiles.candidates(author.id).items.single().state })
        compose.waitUntil(30_000) { model.candidates.value.items.singleOrNull()?.state == "applied" }
        val applied = hasText("基础资料 · 已采用")
        overview.performScrollToNode(applied)
        compose.onNode(applied).assertIsDisplayed()
        screenshot("candidate-applied")
    }

    private fun verifyDirectorySources(graph: DataGraph, author: Author) {
        fun open() {
            compose.onNodeWithContentDescription("${author.name}的作者操作").performClick()
            compose.onNodeWithText("来源 / 出处").performClick()
            await(hasTestTag("author-sources-draft"))
        }
        open()
        compose.onNodeWithText("保存来源").assertIsNotEnabled()
        compose.onNodeWithTag("author-sources-draft").performTextReplacement("合成渠道")
        compose.onNodeWithText("取消").performClick()
        compose.onNodeWithText("放弃修改").performClick()
        assertTrue(runBlocking { graph.media.author(author.id).acquisitionSources.isEmpty() })
        open()
        compose.onNodeWithTag("author-sources-draft").performTextReplacement("合成渠道")
        compose.onNodeWithText("保存来源").performClick()
        compose.waitUntil(30_000) { compose.onAllNodesWithText("保存来源").fetchSemanticsNodes().isEmpty() }
        assertEquals(listOf("合成渠道"), runBlocking { graph.media.author(author.id).acquisitionSources })
    }

    private fun await(matcher: SemanticsMatcher) = compose.waitUntil(30_000) {
        compose.onAllNodes(matcher).fetchSemanticsNodes().isNotEmpty()
    }

    private fun screenshot(state: String) { check(captureCloudScreenshot("author-${width}dp-$state.png")) }
    private fun key() = UUID.randomUUID().toString()
}
