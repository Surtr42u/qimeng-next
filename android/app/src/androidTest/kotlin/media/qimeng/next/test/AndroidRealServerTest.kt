package media.qimeng.next.test

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.content.Intent
import android.net.Uri
import android.os.ParcelFileDescriptor
import android.provider.DocumentsContract
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
import media.qimeng.next.model.TabBarMaterial
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
@org.junit.FixMethodOrder(org.junit.runners.MethodSorters.NAME_ASCENDING)
class AndroidRealServerTest {
    @get:Rule val compose = createAndroidComposeRule<MainActivity>()

    @Test(timeout = 480_000)
    fun loginUploadTagAndBrowseThroughRealServer() {
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
        copyCloudVideo(videoUri)
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
        compose.onNodeWithTag("navigation:home").assertIsSelected()
        val model = androidx.lifecycle.ViewModelProvider(compose.activity)[media.qimeng.next.LibraryViewModel::class.java]
        try {
            compose.waitUntil(30_000) { compose.onAllNodesWithContentDescription(picture.name).fetchSemanticsNodes().isNotEmpty() }
        } catch (failure: Throwable) {
            saveScreenshot("unverified-real-server-home.png")
            val state = model.assets("home:0").value
            val served = runBlocking { graph.media.assets(model.query("home:0")).items }
            throw AssertionError("Home after completed upload: loaded=${state.loaded} loading=${state.loading} " +
                "error=${state.error} observedItems=${state.items.size} hasPicture=${state.items.any { it.id == picture.id }} " +
                "serverItems=${served.size} serverHasPicture=${served.any { it.id == picture.id }}", failure)
        }
        screenshotWithMedia("real-server-home.png")
        captureClassicHome(graph, picture.name)
        compose.onNodeWithContentDescription(picture.name).performClick()
        compose.waitUntil(30_000) { compose.onAllNodesWithContentDescription("标签").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithContentDescription("标签").performClick()
        compose.onNodeWithTag("detail-new-tag").performTextInput("安卓云端标签")
        compose.onNodeWithText("添加").performClick()
        compose.waitUntil(20_000) { compose.onAllNodesWithText("已选 1 个").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("取消").performClick()
        // The installed baseline opens a fresh sheet and selects an existing vocabulary entry, with the IME closed.
        compose.onNodeWithContentDescription("标签").performClick()
        compose.waitUntil(20_000) { compose.onAllNodesWithText("安卓云端标签").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("安卓云端标签").performClick()
        compose.onNodeWithText("已选 1 个").assertIsDisplayed()
        screenshot("real-server-tag-sheet.png")
        compose.onNodeWithText("保存").performClick()
        await("save-tags", graph) { graph.media.asset(picture.id).tags == listOf("安卓云端标签") }
        compose.onNodeWithContentDescription("收藏").performClick()
        await("favorite", graph) { graph.media.asset(picture.id).favorite }
        compose.waitUntil(20_000) { model.asset.value?.favorite == true && !model.busy.value }
        compose.onNodeWithContentDescription("点赞").assertIsEnabled().performClick()
        await("today-like", graph) { graph.media.asset(picture.id).let { it.favorite && it.liked && it.likeCount == picture.likeCount + 1 } }
        compose.waitUntil(20_000) { model.asset.value?.liked == true && !model.busy.value }
        compose.onNodeWithContentDescription("点赞").assertIsEnabled().performClick()
        await("cancel-today-like", graph) { graph.media.asset(picture.id).let { it.favorite && !it.liked && it.likeCount == picture.likeCount } }
        screenshot("real-server-detail.png")

        compose.onNodeWithContentDescription("返回").performClick()
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
    }

    @Test(timeout = 240_000)
    fun playRealVideoThroughTheLandscapeWindow() {
        val graph = runBlocking { isolatedServerGraph() }
        val video = runBlocking {
            graph.media.assets(AssetQuery(mediaType = "video")).items.firstOrNull { it.originalName == "android-synthetic.mp4" } ?: run {
                val uri = Uri.parse("content://media.qimeng.next.test.fixture/android-synthetic.mp4")
                copyCloudVideo(uri)
                val library = graph.media.createLibrary("云端独立播放样本", false)
                val input = graph.uploads.receive(listOf(uri)).single()
                graph.uploads.enqueue(library.id, "", UploadMode.ORDINARY, inputIds = listOf(input))
                withTimeout(90_000) {
                    while (graph.queue.state.value!!.transfers.single { it.id == input }.state != TransferState.SUCCEEDED) {
                        check(graph.queue.state.value!!.transfers.single { it.id == input }.state != TransferState.FAILED)
                        delay(350)
                    }
                }
                graph.media.asset(requireNotNull(graph.queue.state.value!!.transfers.single { it.id == input }.assetId))
            }
        }
        val originalOrientation = playbackOrientation()
        var resumePosition = 0L
        repeat(2) { index ->
            val suffix = if (index == 0) "" else "-resumed"
            val before = runBlocking { graph.media.asset(video.id) }
            assertEquals(video.version, before.version)
            compose.activityRule.scenario.onActivity { androidx.lifecycle.ViewModelProvider(it)[media.qimeng.next.LibraryViewModel::class.java].openAsset(video.id) }
            compose.waitUntil(30_000) { compose.onAllNodesWithContentDescription("视频播放区域").fetchSemanticsNodes().isNotEmpty() }
            compose.activityRule.scenario.onActivity { assertNull(it.findViewById<android.view.View>(media.qimeng.next.R.id.video_playback_window)) }
            compose.onNodeWithContentDescription("视频播放区域").performTouchInput { click(center) }
            if (index == 1) {
                var position: Long? = null
                compose.waitUntil(10_000) {
                    compose.activityRule.scenario.onActivity { activity ->
                        val player = activity.findViewById<androidx.media3.ui.PlayerView>(media.qimeng.next.R.id.video_playback_window)?.player
                        position = player?.takeIf { it.currentMediaItem != null }?.currentPosition
                    }
                    position != null
                }
                assertTrue("The new player must start at the position read back from Go", requireNotNull(position) >= resumePosition)
            }
            compose.waitUntil(30_000) { compose.onAllNodesWithContentDescription("进入全屏").fetchSemanticsNodes().isNotEmpty() }
            compose.onNodeWithContentDescription("进入全屏").performClick()
            compose.onNodeWithContentDescription("退出全屏").assertIsDisplayed()
            compose.waitUntil(10_000) { playbackOrientation().let {
                it.request == android.content.pm.ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE &&
                    it.configuration == android.content.res.Configuration.ORIENTATION_LANDSCAPE
            } }
            captureDecoderSurface(compose.activity, suffix)
            screenshotWithMedia("real-server-video-fullscreen$suffix.png", landscape = true)
            await("video-play-event$suffix", graph) { (graph.media.asset(video.id).playCount ?: 0L) > (before.playCount ?: 0L) }
            assertTrue(runBlocking { graph.media.history().items.any { it.assetId == video.id && it.kind == "view" } })
            compose.onNodeWithContentDescription("退出全屏").performClick()
            try {
                compose.waitUntil(15_000) { playbackOrientation() == originalOrientation }
            } catch (failure: ComposeTimeoutException) {
                throw AssertionError("Leaving fullscreen should restore $originalOrientation, observed ${playbackOrientation()}", failure)
            }
            if (index == 0) {
                compose.activityRule.scenario.onActivity { it.onBackPressedDispatcher.onBackPressed() }
                compose.waitUntil(10_000) { compose.onAllNodesWithContentDescription("返回").fetchSemanticsNodes().isNotEmpty() }
                compose.onNodeWithContentDescription("返回").performClick()
                await("video-position", graph) { graph.media.asset(video.id).let { asset ->
                    resumePosition = asset.positionMs
                    asset.positionVersion == video.version && resumePosition > 0L
                } }
            }
        }
    }

    private data class PlaybackOrientation(val request: Int, val configuration: Int, val rotation: Int)

    private fun playbackOrientation(): PlaybackOrientation {
        lateinit var snapshot: PlaybackOrientation
        compose.activityRule.scenario.onActivity { activity ->
            snapshot = PlaybackOrientation(activity.requestedOrientation, activity.resources.configuration.orientation,
                requireNotNull(activity.window.decorView.display).rotation)
        }
        return snapshot
    }

    @Test(timeout = 240_000)
    fun readOnlyArchivePreservesOriginalAfterIndependentBackup() = runBlocking {
        archiveRoundTrip(isolatedServerGraph())
    }

    private suspend fun isolatedServerGraph(): DataGraph {
        val instrument = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        val address = requireNotNull(args.getString("qimengServerUrl"))
        require(address == "http://127.0.0.1:18481/")
        val graph = DataGraph.get(instrument.targetContext)
        graph.awaitRecovery()
        if (graph.settings.session.value?.baseUrl != address) graph.media.login(address, requireNotNull(args.getString("qimengToken")))
        return graph
    }

    @Test fun wrongInstanceMaintenanceFolderKeepsItsGrantTemporary() = runBlocking {
        val instrument = InstrumentationRegistry.getInstrumentation()
        val resolver = instrument.targetContext.contentResolver
        val fixture = resolver.call(Uri.parse("content://media.qimeng.next.test.fixture"), "newMaintenanceFixture", null, null)
        val tree = Uri.parse(requireNotNull(fixture?.getString("tree")))
        val root = DocumentsContract.buildDocumentUriUsingTree(tree, DocumentsContract.getTreeDocumentId(tree))
        val slot = requireNotNull(DocumentsContract.createDocument(resolver, root, DocumentsContract.Document.MIME_TYPE_DIR, "当前"))
        val record = requireNotNull(DocumentsContract.createDocument(resolver, slot, "application/json", ".绮梦下一代清单.json"))
        val hashes = org.json.JSONObject()
        listOf("archive", "master", "report", "index").forEach { hashes.put(it, "0".repeat(64)) }
        val generation = org.json.JSONObject().put("instanceId", "foreign-synthetic-instance")
            .put("businessRevision", "1").put("backupId", "synthetic-backup").put("hashes", hashes)
        requireNotNull(resolver.openOutputStream(record)).use { it.write(generation.toString().toByteArray(Charsets.UTF_8)) }
        val graph = isolatedServerGraph()
        val target = requireNotNull(graph.settings.session.value)
        val previous = graph.backups.folder(target)
        val access = Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION
        assertFalse(resolver.persistedUriPermissions.any { it.uri == tree })
        try {
            graph.backups.bind(target, tree, access)
            fail("A foreign maintenance generation was accepted")
        } catch (failure: IllegalStateException) {
            assertTrue("Expected the foreign-instance rejection", failure.message.orEmpty().contains("另一影库"))
        }
        assertFalse("Rejecting a foreign folder must preserve its temporary-only grant", resolver.persistedUriPermissions.any { it.uri == tree })
        assertEquals(previous, graph.backups.folder(target))
        compose.activityRule.scenario.onActivity { androidx.lifecycle.ViewModelProvider(it)[media.qimeng.next.LibraryViewModel::class.java].navigate("maintenance") }
        compose.waitUntil(20_000) { compose.onAllNodes(hasText("设备领取") and isEnabled()).fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("设备领取").performClick()
        compose.onNodeWithText("设备结果包").assertExists()
        compose.onNodeWithContentDescription("复制本机影库标识").assertDoesNotExist()
        compose.onNodeWithText("选择本机目标视频").assertDoesNotExist()
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
    private fun captureClassicHome(graph: DataGraph, picture: String) {
        val original = graph.settings.preferences.value
        assertEquals(TabBarMaterial.LIQUID, original.material)
        try {
            compose.onNodeWithTag("navigation:me").performClick()
            compose.waitUntil(20_000) { compose.onAllNodesWithTag("profile-settings-list").fetchSemanticsNodes().isNotEmpty() }
            compose.onNodeWithTag("profile-settings-list").performScrollToNode(hasText("外观"))
            compose.onNodeWithText("外观").performClick()
            compose.onNodeWithText("经典").performScrollTo().performClick()
            compose.waitUntil(10_000) { graph.settings.preferences.value.material == TabBarMaterial.CLASSIC }
            compose.onNodeWithContentDescription("返回").performClick()
            compose.onNodeWithTag("navigation:home").performClick()
            compose.waitUntil(30_000) { compose.onAllNodesWithContentDescription(picture).fetchSemanticsNodes().isNotEmpty() }
            compose.onNodeWithTag("navigation:home").assertIsSelected()
            screenshotWithMedia("real-server-home-classic.png")
        } finally {
            graph.settings.savePreferences(original)
            compose.waitForIdle()
        }
    }
    @androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
    private fun screenshotWithMedia(name: String, landscape: Boolean = false) {
        var observed = ""
        try {
        compose.waitUntil(30_000) {
            media.qimeng.next.testsupport.captureCloudScreenshot(name) { bitmap ->
                observed = "${bitmap.width}×${bitmap.height}"
                if (landscape && bitmap.width <= bitmap.height) return@captureCloudScreenshot false
                // Both the PNG and H.264 fixtures contain saturated colors; the surrounding UI and shutter are neutral.
                val readable = if (bitmap.config == Bitmap.Config.HARDWARE) requireNotNull(bitmap.copy(Bitmap.Config.ARGB_8888, false)) else bitmap
                try {
                    val colored = saturatedSamples(readable)
                    observed += " saturatedSamples=$colored"
                    if (landscape && colored >= 30) android.util.Log.i("QimengFrameEvidence", "compositedWindow $observed")
                    colored >= 30
                } finally {
                    if (readable !== bitmap) readable.recycle()
                }
            }
        }
        } catch (failure: Throwable) {
            saveScreenshot("unverified-$name")
            var playback = ""
            if (landscape) compose.activityRule.scenario.onActivity { activity ->
                val view = activity.findViewById<androidx.media3.ui.PlayerView>(media.qimeng.next.R.id.video_playback_window)
                val player = view?.player as? androidx.media3.exoplayer.ExoPlayer
                playback = " playerVisible=${view?.isShown} playerBounds=${view?.width}x${view?.height}" +
                    " playerState=${player?.playbackState} firstFrameCount=${player?.videoDecoderCounters?.renderedOutputBufferCount}" +
                    " mediaPosition=${player?.currentPosition} error=${player?.playerError?.errorCode}"
            }
            throw AssertionError("Window capture $name failed. Last sample $observed.$playback", failure)
        }
    }
    private fun saveScreenshot(name: String) {
        media.qimeng.next.testsupport.captureCloudScreenshot(name)
    }
}

internal fun copyCloudVideo(uri: Uri) {
    val instrument = InstrumentationRegistry.getInstrumentation()
    ParcelFileDescriptor.AutoCloseInputStream(instrument.uiAutomation.executeShellCommand("cat /data/local/tmp/qimeng-next-test.mp4")).use { input ->
        requireNotNull(instrument.targetContext.contentResolver.openOutputStream(uri, "w")).use { output ->
            assertTrue("The cloud H.264 fixture must have bytes", input.copyTo(output) in 100L..8_000_000L)
        }
    }
}
