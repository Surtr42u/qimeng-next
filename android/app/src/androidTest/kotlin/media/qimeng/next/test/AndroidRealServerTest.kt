package media.qimeng.next.test

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.content.Intent
import android.net.Uri
import android.os.ParcelFileDescriptor
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import media.qimeng.next.MainActivity
import media.qimeng.next.data.DataGraph
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.model.TransferState
import media.qimeng.next.model.UploadMode
import media.qimeng.next.model.OriginalInput
import media.qimeng.next.model.Transfer
import media.qimeng.next.model.TransferBatch
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import java.io.File
import java.util.UUID

/** Run only against the same-SHA isolated Go process, using explicit cloud runner arguments. */
class AndroidRealServerTest {
    @get:Rule val compose = createAndroidComposeRule<MainActivity>()

    @Test(timeout = 480_000)
    fun loginUploadTagRecommendAndPlayThroughRealServer() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        val server = requireNotNull(args.getString("qimengServerUrl")) { "qimengServerUrl runner argument is required" }
        require(server == "http://127.0.0.1:18481/") { "Only the isolated cloud tunnel is allowed" }
        val token = requireNotNull(args.getString("qimengToken")) { "A synthetic server token is required" }
        val context = instrumentation.targetContext
        val graph = DataGraph.get(context)

        compose.onNodeWithText("NAS / 电脑").performClick()
        compose.onNodeWithText("手动填写地址").performClick()
        compose.onNodeWithTag("login-address").performTextInput(server)
        compose.onNodeWithTag("login-password").performTextInput(token)
        compose.waitUntil(30_000) { compose.onAllNodes(hasText("连接影库") and isEnabled()).fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithTag("login-password").performImeAction()
        await("login", graph) {
            val errors = compose.onAllNodesWithTag("login-error").fetchSemanticsNodes()
                .flatMap { it.config[SemanticsProperties.Text] }.joinToString { it.text }
            check(errors.isEmpty()) { "Login UI error: $errors" }
            graph.settings.session.value != null && graph.queue.state.value != null
        }
        assertTrue("Cloud test requires an empty transfer journal", graph.queue.state.value!!.transfers.isEmpty())
        val library = runBlocking { graph.media.createLibrary("Android 云端合成影库", false) }

        val imageUri = Uri.parse("content://media.qimeng.next.test.fixture/android-synthetic.png")
        val videoUri = Uri.parse("content://media.qimeng.next.test.fixture/android-synthetic.mp4")
        val bitmap = Bitmap.createBitmap(160, 240, Bitmap.Config.ARGB_8888)
        Canvas(bitmap).apply {
            drawColor(Color.rgb(224, 180, 112))
            drawRect(0f, 0f, 80f, 240f, Paint().apply { color = Color.rgb(72, 112, 190) })
            drawCircle(80f, 120f, 46f, Paint().apply { color = Color.WHITE })
        }
        context.contentResolver.openOutputStream(imageUri, "w")!!.use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        bitmap.recycle()
        ParcelFileDescriptor.AutoCloseInputStream(instrumentation.uiAutomation.executeShellCommand("cat /data/local/tmp/qimeng-next-test.mp4")).use { input ->
            context.contentResolver.openOutputStream(videoUri, "w")!!.use { output ->
                val bytes = input.copyTo(output)
                assertTrue("Push a nonempty synthetic H.264 fixture before this test", bytes in 100L..8_000_000L)
            }
        }
        val manualDraft = runBlocking {
            graph.settings.saveUploadLibrary(graph.settings.session.value!!, library.id)
            graph.uploads.receive(listOf(imageUri)).single()
        }
        compose.activityRule.scenario.onActivity { activity ->
            activity.startActivity(Intent(activity, MainActivity::class.java).apply {
                action = Intent.ACTION_SEND_MULTIPLE
                type = "image/*"
                putParcelableArrayListExtra(Intent.EXTRA_STREAM, arrayListOf(imageUri,
                    Uri.parse("content://media.qimeng.next.test.fixture/unreadable-selection"), videoUri))
                addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_GRANT_READ_URI_PERMISSION)
            })
        }
        await("manifest-and-upload", graph) {
            val transfers = graph.queue.state.value!!.transfers
            assertTrue("A real upload failed: ${transfers.joinToString { it.message }}", transfers.none { it.state == TransferState.FAILED })
            transfers.size == 2 && transfers.all { it.state == TransferState.SUCCEEDED }
        }
        val assets = runBlocking { graph.media.assets(AssetQuery(libraryId = library.id)).items }
        assertEquals(2, assets.size)
        val picture = assets.single { it.mediaType == "image" }
        val video = assets.single { it.mediaType == "video" }
        assertTrue(picture.thumbnailUrl.isNotEmpty())
        assertTrue((video.durationMs ?: 0) > 0)
        val batch = graph.queue.state.value!!.batches.single()
        assertEquals(listOf(manualDraft), graph.queue.state.value!!.drafts.map { it.id })
        assertEquals(listOf(manualDraft), graph.queue.state.value!!.pendingInputs.map { it.id })
        assertEquals(3, batch.inputs.size)
        assertNull(batch.inputs[1].originalFileName)
        assertEquals(batch.key, graph.queue.state.value!!.intakeFailures.single().batchKey)
        screenshot("real-server-shared-upload.png")
        compose.onNodeWithTag("upload-list").performScrollToNode(hasText("锁定"))
        compose.onNodeWithTag("draft-name:$manualDraft").performTextReplacement("手选保留")
        await("independent-edited-stem", graph) { graph.queue.state.value?.drafts?.singleOrNull()?.name == "手选保留.png" }
        assertEquals("android-synthetic.png", graph.queue.state.value!!.drafts.single().originalName)
        screenshot("real-server-upload-locked-extension.png")
        assertEquals(64, batch.manifestFingerprint!!.length)
        val originalReport = runBlocking { graph.media.report(batch.serverId!!) }
        assertEquals(3, originalReport.items.size)
        assertEquals("pending", originalReport.items[1].state)
        assertNotEquals("completed", originalReport.state)

        val cancelledId = UUID.randomUUID().toString()
        val cancelledBatch = TransferBatch(UUID.randomUUID().toString(), graph.settings.session.value!!, library.id, UploadMode.ORDINARY,
            "Synthetic cancelled original input", listOf(OriginalInput(cancelledId, "cancelled.png", picture.bytes, batch.inputs[0].expectedHash, "known")))
        runBlocking { graph.queue.update { it.copy(batches = it.batches + cancelledBatch,
            transfers = it.transfers + Transfer(cancelledId, cancelledBatch.target, library.id, "cancelled.png", "image/png", picture.bytes,
                "unused-cancelled-fixture", UploadMode.ORDINARY, null, cancelledBatch.key, cancelledBatch.label, state = TransferState.CANCELLED)) } }
        graph.uploads.schedule()
        await("cancel-unstarted-input", graph) { graph.queue.state.value!!.transfers.single { it.id == cancelledId }.cancelConfirmed }
        val stopped = graph.queue.state.value!!.transfers.single { it.id == cancelledId }
        assertNull(stopped.uploadId)
        assertEquals("cancelled", runBlocking { graph.media.report(stopped.batchId!!).items.single().state })

        compose.onNodeWithContentDescription("返回").performClick()
        compose.onNodeWithTag("navigation:home").performClick()
        compose.onNodeWithTag("media-browser:home:0").performTouchInput { swipeDown() }
        compose.waitUntil(30_000) { compose.onAllNodesWithContentDescription(picture.name).fetchSemanticsNodes().isNotEmpty() }
        screenshotWithMedia("real-server-home.png")
        compose.onNodeWithContentDescription(picture.name).performClick()
        compose.waitUntil(30_000) { compose.onAllNodesWithContentDescription("标签").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithContentDescription("标签").performClick()
        compose.onNodeWithText("新建标签").performTextInput("安卓云端标签")
        compose.onNodeWithText("添加").performClick()
        compose.waitUntil(20_000) { compose.onAllNodesWithText("安卓云端标签").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("保存").performClick()
        await("save-tags", graph) { graph.media.asset(picture.id).tags == listOf("安卓云端标签") }
        compose.onNodeWithContentDescription("收藏").performClick()
        await("favorite", graph) { graph.media.asset(picture.id).favorite }
        screenshot("real-server-detail.png")

        compose.onNodeWithContentDescription("返回").performClick()
        compose.onNodeWithContentDescription(video.name).performClick()
        compose.waitUntil(30_000) { compose.onAllNodesWithContentDescription("视频播放区域").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithContentDescription("视频播放区域").performTouchInput { click(center) }
        compose.waitUntil(30_000) { compose.onAllNodesWithContentDescription("进入全屏").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithContentDescription("进入全屏").performClick()
        compose.onNodeWithContentDescription("退出全屏").assertIsDisplayed()
        screenshotWithMedia("real-server-video-fullscreen.png", landscape = true)
        await("video-play-event", graph) { (graph.media.asset(video.id).playCount ?: 0L) > (video.playCount ?: 0L) }
        assertTrue(runBlocking { graph.media.history().items.any { it.assetId == video.id && it.kind == "view" } })
        compose.onNodeWithContentDescription("退出全屏").performClick()
        val recommendation = runBlocking { graph.media.assets(AssetQuery(source = "recommendations", seed = 42)) }
        assertTrue(recommendation.items.any { it.id == picture.id })
        repeat(2) {
            if (compose.onAllNodesWithTag("navigation:albums").fetchSemanticsNodes().isEmpty()) {
                compose.activityRule.scenario.onActivity { activity -> activity.onBackPressedDispatcher.onBackPressed() }
                compose.waitForIdle()
            }
        }
        compose.onNodeWithTag("navigation:albums").performClick()
        compose.waitUntil(30_000) { compose.onAllNodesWithText("2 文件").fetchSemanticsNodes().isNotEmpty() }
        screenshot("real-server-albums.png")
        compose.onNodeWithText("分区 (2)").performClick()
        compose.onNodeWithText("常规 (2)").assertExists()
        screenshot("real-server-albums-expanded.png")
        compose.onNodeWithTag("navigation:home").performClick()
        compose.onNodeWithContentDescription("筛选").performClick()
        compose.onNodeWithText("升序").performScrollTo().performClick()
        screenshot("real-server-filter-sheet.png")
        compose.onNodeWithText("应用筛选").performClick()
        compose.onNodeWithContentDescription(picture.name).assertExists()
        compose.onNodeWithText("COS").performClick()
        val model = androidx.lifecycle.ViewModelProvider(compose.activity)[media.qimeng.next.LibraryViewModel::class.java]
        compose.waitUntil(30_000) { model.query("home:1").let { it.kind == "cos" && it.source == "assets" && it.order == "asc" } }
        compose.onNodeWithContentDescription("筛选").performClick()
        compose.onNodeWithText("重置").performClick()
        compose.waitUntil(30_000) { model.query("home:1").source == "recommendations" }
        compose.activityRule.scenario.onActivity { model.navigate("authors") }
        compose.onNodeWithText("新建作者").performClick()
        compose.onNodeWithTag("author-name-draft").performTextInput("合成作者 / 合成别名")
        compose.onNodeWithText("确定").performClick()
        await("author-directory-create", graph) { graph.media.authors(query = "合成别名").items.any { it.name == "合成作者" } }
        compose.onNodeWithTag("author-search").performTextReplacement("合成别名")
        compose.waitUntil(30_000) { compose.onAllNodesWithText("合成作者").fetchSemanticsNodes().isNotEmpty() }
        screenshot("real-server-author-directory.png")
        runBlocking { archiveRoundTrip(graph) }
    }

    private fun await(stage: String, graph: DataGraph, condition: suspend () -> Boolean) = runBlocking {
        try { withTimeout(120_000) { while (!condition()) delay(400) } }
        catch (failure: Throwable) {
            saveScreenshot("failed-$stage.png")
            val queue = graph.queue.state.value
            // Synthetic test state only. Never serialize the connection, credentials, URI or journal.
            throw AssertionError("Stage $stage failed; authenticated=${graph.settings.session.value != null}; " +
                "queueLoaded=${queue != null}; queueError=${graph.queue.error.value}; " +
                "transfers=${queue?.transfers?.map { "${it.state}:${it.offset}/${it.bytes}:${it.message}" }}; " +
                "manifests=${queue?.batches?.map { "created=${it.serverId != null},sealed=${it.manifestFingerprint != null}" }}", failure)
        }
    }
    private fun screenshot(name: String) {
        compose.waitForIdle()
        saveScreenshot(name)
    }
    private fun screenshotWithMedia(name: String, landscape: Boolean = false) {
        compose.waitUntil(30_000) {
            media.qimeng.next.testsupport.captureCloudScreenshot(name) { bitmap ->
                if (landscape && bitmap.width <= bitmap.height) return@captureCloudScreenshot false
                // Both the PNG and H.264 fixtures contain saturated colors; the surrounding UI and shutter are neutral.
                val readable = if (bitmap.config == Bitmap.Config.HARDWARE) requireNotNull(bitmap.copy(Bitmap.Config.ARGB_8888, false)) else bitmap
                try {
                    var colored = 0
                    for (y in readable.height / 8 until readable.height * 7 / 8 step 8) for (x in 8 until readable.width - 8 step 8) {
                        val pixel = readable.getPixel(x, y)
                        val r = Color.red(pixel); val g = Color.green(pixel); val b = Color.blue(pixel)
                        if (maxOf(r, g, b) - minOf(r, g, b) > 75) colored++
                    }
                    colored >= 30
                } finally {
                    if (readable !== bitmap) readable.recycle()
                }
            }
        }
    }
    private fun saveScreenshot(name: String) {
        media.qimeng.next.testsupport.captureCloudScreenshot(name)
    }
}
