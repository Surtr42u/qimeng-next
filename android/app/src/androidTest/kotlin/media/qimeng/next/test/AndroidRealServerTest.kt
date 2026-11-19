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
import media.qimeng.next.model.UploadOptions
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
        editVisibleField("draft-name:$manualDraft", "手选保留")
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
        editVisibleField("detail-new-tag", "安卓云端标签")
        val addTag = hasText("添加") and hasClickAction() and isEnabled()
        compose.waitUntil(20_000) { !model.busy.value && compose.onAllNodes(addTag).fetchSemanticsNodes().isNotEmpty() }
        compose.onNode(addTag).assertIsDisplayed().performClick()
        try {
            compose.waitUntil(20_000) { compose.onAllNodesWithText("已选 1 个").fetchSemanticsNodes().isNotEmpty() }
        } catch (failure: Throwable) {
            saveScreenshot("unverified-real-server-tag-create.png")
            val created = runBlocking { "安卓云端标签" in graph.media.tags().items }
            throw AssertionError("Tag after Add: storedInVocabulary=$created busy=${model.busy.value} errorPresent=${model.message.value != null}", failure)
        }
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
    fun maintainsExtractionVocabularyFromDataManagement() {
        val graph = runBlocking { isolatedServerGraph() }
        val model = compose.runOnIdle { androidx.lifecycle.ViewModelProvider(compose.activity)[media.qimeng.next.LibraryViewModel::class.java] }
        compose.runOnIdle { model.selectTab(3) }
        compose.onNodeWithTag("profile-settings-list").performScrollToNode(hasText("数据管理"))
        compose.onNodeWithText("数据管理").performClick()
        compose.onNodeWithText("词表维护").performScrollTo().performClick()
        compose.onNodeWithText("原名提取词表").performClick()
        val list = compose.onNodeWithTag("extraction-vocabulary-list")
        fun press(label: String) {
            val node = hasText(label) and hasClickAction() and isEnabled()
            list.performScrollToNode(node)
            compose.onNode(node).performClick()
        }
        fun add(button: String, value: String) {
            press(button)
            editVisibleField("extraction-term", value)
            compose.onNodeWithText("确定").performClick()
        }
        val target = requireNotNull(graph.settings.session.value)
        val before = runBlocking { graph.extractionVocabulary.read(target) }
        val suffix = UUID.randomUUID().toString().take(8)
        val group = "合成出处$suffix"
        val variant = "合成变体$suffix"
        val role = "合成角色$suffix"
        val alias = "合成别名$suffix"
        val stopWord = "合成停用$suffix"
        val attemptedWord = "保留草稿$suffix"
        val externalWord = "外部编辑$suffix"
        compose.waitUntil(30_000) { compose.onAllNodesWithText("添加停用词").fetchSemanticsNodes().isNotEmpty() }
        add("添加停用词", stopWord)
        add("添加出处组", group)
        list.performScrollToNode(hasText(group))
        compose.onNodeWithTag("extraction-group:$group").performClick()
        add("添加变体", variant)
        add("添加角色", role)
        press("角色别名")
        add("添加别名", alias)
        press("保存修改")
        await("extraction-vocabulary-rebuild", graph) {
            val current = graph.extractionVocabulary.read(target)
            current.revision == before.revision + 1 && current.rebuildState == "completed"
        }
        val saved = runBlocking { graph.extractionVocabulary.read(target) }
        assertTrue(stopWord in saved.layer.stopWords)
        assertEquals(listOf(variant), saved.layer.groups.single { it.canonical == group }.variants)
        assertEquals(listOf(alias), saved.layer.groups.single { it.canonical == group }.roles.single { it.canonical == role }.aliases)
        list.performScrollToIndex(0)
        compose.waitUntil(15_000) {
            compose.onAllNodesWithText("已保存 · 服务端重算完成 · 修订 ${saved.revision}").fetchSemanticsNodes().isNotEmpty()
        }
        screenshot("real-server-extraction-vocabulary.png")

        add("添加停用词", attemptedWord)
        val newer = runBlocking { graph.extractionVocabulary.save(target, saved.revision,
            saved.layer.copy(stopWords = saved.layer.stopWords + externalWord), UUID.randomUUID().toString()) }
        press("保存修改")
        list.performScrollToIndex(0)
        compose.waitUntil(15_000) {
            compose.onAllNodesWithText("服务端词表已被修改。本次草稿仍保留。").fetchSemanticsNodes().isNotEmpty()
        }
        list.performScrollToNode(hasText(attemptedWord))
        compose.onNodeWithText(attemptedWord).assertExists()
        assertEquals(newer.revision, runBlocking { graph.extractionVocabulary.read(target) }.revision)
        press("读取服务端新词表")
        compose.onNodeWithText("放弃并读取").performClick()
        list.performScrollToIndex(0)
        compose.waitUntil(15_000) { compose.onAllNodesWithText(externalWord).fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText(attemptedWord).assertDoesNotExist()
    }

    @Test(timeout = 240_000)
    fun mixedProviderInputsRetainEveryDispositionAndOnlyUploadGoodMedia() = runBlocking {
        mixedUploadRoundTrip(isolatedServerGraph())
    }

    @Test(timeout = 240_000)
    fun ordinaryUploadUsesAuthorSourceAndFileNameSuggestions() {
        val graph = runBlocking { isolatedServerGraph() }
        val target = requireNotNull(graph.settings.session.value)
        assertFalse(runBlocking { graph.media.capabilities().aiConfigured })
        val suffix = UUID.randomUUID().toString().take(8)
        val family = "上传建议-$suffix"
        val authorChannel = "作者渠道$suffix"
        val assetChannel = "作品渠道$suffix"
        val library = runBlocking { graph.media.createLibrary("普通上传 $suffix", false) }
        val alias = "检索别名$suffix"
        val author = runBlocking { graph.media.createNamedAuthor(target, listOf("合成作者$suffix", alias), UUID.randomUUID().toString()) }
        setSyntheticAcquisitionOptions(graph, target, authorChannel, assetChannel)
        val seedKey = UUID.randomUUID().toString()
        val (source, _) = writeMetadataImage("seed-$suffix")
        val seed = runBlocking {
            graph.queue.state.value!!.drafts.filter { it.intakeGroup == null }.forEach { graph.uploads.discardDraft(it.id) }
            val id = graph.uploads.receive(listOf(source), intakeGroup = seedKey).single()
            graph.uploads.renameDraft(id, "$family 01.png")
            graph.uploads.enqueue(library.id, "", UploadMode.ORDINARY, target = target, inputIds = listOf(id), batchKey = seedKey)
            id
        }
        await("ordinary-name-seed", graph) { graph.queue.state.value!!.transfers.single { it.id == seed }.state == TransferState.SUCCEEDED }
        val (_, originalBytes) = writeMetadataImage("new-$suffix")
        val model = compose.runOnIdle { androidx.lifecycle.ViewModelProvider(compose.activity)[media.qimeng.next.LibraryViewModel::class.java] }
        compose.runOnIdle { model.selectTab(3) }
        compose.onNodeWithTag("profile-settings-list").performScrollToNode(hasText("数据管理"))
        compose.onNodeWithText("数据管理").performClick()
        compose.onNodeWithText("上传文件").performClick()
        val list = compose.onNodeWithTag("upload-list")
        compose.waitUntil(30_000) { model.libraries.value.any { it.id == library.id } }
        list.performScrollToNode(hasText(library.name))
        compose.onNodeWithText(library.name).performClick()
        compose.waitUntil(20_000) { model.uploadLibrary.value == library.id }
        compose.runOnIdle { model.receiveFiles(listOf(source)) }
        await("ordinary-suggest-draft", graph) { graph.queue.state.value?.drafts?.count { it.intakeGroup == null } == 1 }
        val draft = graph.queue.state.value!!.drafts.single { it.intakeGroup == null }
        list.performScrollToNode(hasText("作者与来源"))
        compose.onNodeWithText("作者与来源").performClick()
        list.performScrollToNode(hasText("选择作者"))
        compose.onNodeWithText("选择作者").performClick()
        editVisibleField("upload-author-search", alias)
        val authorTag = "upload-author-search:${author.id}"
        compose.waitUntil(20_000) { compose.onAllNodesWithTag(authorTag).fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithTag(authorTag).performClick()
        compose.onNodeWithText("应用").performClick()
        list.performScrollToNode(hasTestTag("upload-author-source:$authorChannel"))
        compose.onNodeWithTag("upload-author-source:$authorChannel").performClick()
        list.performScrollToNode(hasTestTag("upload-asset-source:$assetChannel"))
        compose.onNodeWithTag("upload-asset-source:$assetChannel").performClick()
        list.performScrollToNode(hasTestTag("draft-name:${draft.id}"))
        editVisibleField("draft-name:${draft.id}", family)
        val suggested = "$family 02.png"
        compose.waitUntil(20_000) { compose.onAllNodesWithText(suggested).fetchSemanticsNodes().isNotEmpty() }
        list.performScrollToNode(hasText(suggested))
        screenshot("real-server-ordinary-upload-suggestion.png")
        compose.onNodeWithText(suggested).performClick()
        compose.onNodeWithTag("draft-name:${draft.id}").assertTextEquals("$family 02")
        compose.onNodeWithText("锁定").assertExists()
        list.performScrollToNode(hasText("开始上传（1 项）"))
        compose.onNodeWithText("开始上传（1 项）").performClick()
        await("ordinary-upload-readback", graph) { graph.queue.state.value!!.transfers.any { it.id == draft.id && it.state == TransferState.SUCCEEDED } }
        val transfer = graph.queue.state.value!!.transfers.single { it.id == draft.id }
        val asset = runBlocking { graph.media.asset(requireNotNull(transfer.assetId), target) }
        assertEquals(suggested, asset.name)
        assertEquals(draft.originalName, asset.originalName)
        assertEquals(listOf(author.id), asset.authorIds)
        assertEquals(listOf(assetChannel), asset.acquisitionSources)
        assertEquals(listOf(authorChannel), runBlocking { graph.media.author(author.id, target).acquisitionSources })
        val providerBytes = InstrumentationRegistry.getInstrumentation().targetContext.contentResolver.openInputStream(source)!!.use { it.readBytes() }
        assertArrayEquals(originalBytes, providerBytes)
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
            clickVideoControl("进入全屏")
            compose.waitUntil(10_000) { playbackOrientation().let {
                it.request == android.content.pm.ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE &&
                    it.configuration == android.content.res.Configuration.ORIENTATION_LANDSCAPE
            } }
            captureDecoderSurface(compose.activity, suffix)
            screenshotWithMedia("real-server-video-fullscreen$suffix.png", landscape = true)
            await("video-play-event$suffix", graph) { (graph.media.asset(video.id).playCount ?: 0L) > (before.playCount ?: 0L) }
            assertTrue(runBlocking { graph.media.history().items.any { it.assetId == video.id && it.kind == "view" } })
            clickVideoControl("退出全屏")
            try {
                compose.waitUntil(15_000) { playbackOrientation().let {
                    it.request == android.content.pm.ActivityInfo.SCREEN_ORIENTATION_PORTRAIT &&
                        it.configuration == android.content.res.Configuration.ORIENTATION_PORTRAIT
                } }
            } catch (failure: ComposeTimeoutException) {
                throw AssertionError("Leaving fullscreen should return to portrait, observed ${playbackOrientation()}", failure)
            }
            if (index == 0) {
                compose.waitUntil(10_000) {
                    var position = 0L
                    compose.activityRule.scenario.onActivity { activity ->
                        position = activity.findViewById<androidx.media3.ui.PlayerView>(media.qimeng.next.R.id.video_playback_window)?.player?.currentPosition ?: 0L
                    }
                    position >= 350L
                }
                compose.activityRule.scenario.onActivity { it.onBackPressedDispatcher.onBackPressed() }
                compose.waitUntil(10_000) {
                    var attached = true
                    compose.activityRule.scenario.onActivity { attached = it.findViewById<android.view.View>(media.qimeng.next.R.id.video_playback_window) != null }
                    !attached
                }
                await("video-position", graph) { graph.media.asset(video.id).let { asset ->
                    resumePosition = asset.positionMs
                    asset.positionVersion == video.version && resumePosition > 0L
                } }
                compose.activityRule.scenario.onActivity { it.onBackPressedDispatcher.onBackPressed() }
                compose.waitUntil(10_000) {
                    var leftDetail = false
                    compose.activityRule.scenario.onActivity { activity ->
                        leftDetail = androidx.lifecycle.ViewModelProvider(activity)[media.qimeng.next.LibraryViewModel::class.java].route.value != "asset:${video.id}"
                    }
                    leftDetail
                }
                compose.waitUntil(15_000) { playbackOrientation() == originalOrientation }
            }
        }
    }


    @Test(timeout = 300_000)
    fun videoControlsMatchTheInstalledAppFromNormalEntrances() {
        VideoParityRoundTrip(compose, runBlocking { isolatedServerGraph() }).run()
    }

    private fun clickVideoControl(description: String) {
        var control: android.graphics.Rect? = null
        compose.waitUntil(15_000) {
            compose.activityRule.scenario.onActivity { activity ->
                control = activity.videoControlTouchBounds(description)
            }
            control?.isEmpty == false
        }
        val bounds = requireNotNull(control)
        val down = android.os.SystemClock.uptimeMillis()
        sendVideoTouch(down, android.view.MotionEvent.ACTION_DOWN, bounds.exactCenterX(), bounds.exactCenterY())
        sendVideoTouch(down, android.view.MotionEvent.ACTION_UP, bounds.exactCenterX(), bounds.exactCenterY())
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
    fun queueReportAndMediaLinksReturnToTheirOriginalGoInstance() {
        val graph = runBlocking { isolatedServerGraph() }
        val args = InstrumentationRegistry.getArguments()
        val a = requireNotNull(graph.settings.session.value)
        val b = requireNotNull(args.getString("qimengSourceUrl"))
        require(a.baseUrl == "http://127.0.0.1:18481/" && b == "http://127.0.0.1:18482/")
        val key = UUID.randomUUID().toString()
        val image = Uri.parse("content://media.qimeng.next.test.fixture/mixed-good.png")
        val resolver = InstrumentationRegistry.getInstrumentation().targetContext.contentResolver
        val bitmap = Bitmap.createBitmap(32, 17, Bitmap.Config.ARGB_8888)
        bitmap.eraseColor(Color.rgb(key.hashCode() and 255, 120, 183))
        requireNotNull(resolver.openOutputStream(image, "wt")).use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        bitmap.recycle()
        val id = runBlocking {
            val library = graph.media.createLibrary("队列归属 $key", false)
            val inputs = graph.uploads.receive(listOf(image), intakeGroup = key)
            graph.uploads.enqueue(library.id, "", UploadMode.ORDINARY, target = a, inputIds = inputs, batchKey = key)
            inputs.single()
        }
        await("queue-link-fixture", graph) { graph.queue.state.value!!.transfers.single { it.id == id }.state == TransferState.SUCCEEDED }
        val transfer = graph.queue.state.value!!.transfers.single { it.id == id }
        val batchId = requireNotNull(transfer.batchId)
        val assetId = requireNotNull(transfer.assetId)
        lateinit var model: media.qimeng.next.LibraryViewModel
        compose.activityRule.scenario.onActivity {
            model = androidx.lifecycle.ViewModelProvider(it)[media.qimeng.next.LibraryViewModel::class.java]
            model.navigate("upload")
        }
        val row = hasTestTag("transfer:$id")
        val report = hasText("报告") and hasAnyAncestor(row)
        val media = hasText("查看媒体") and hasAnyAncestor(row)
        fun showButtons() { compose.onNodeWithTag("upload-list").performScrollToNode(report) }
        showButtons()
        compose.onNode(report).assertIsEnabled().performClick()
        compose.waitUntil(20_000) { model.report.value?.id == batchId && !model.busy.value }
        try {
            runBlocking { graph.media.login(b, requireNotNull(args.getString("qimengSourceToken"))) }
            compose.waitUntil(20_000) { model.session.value?.baseUrl == b && model.route.value == "upload" && model.report.value == null }
            showButtons()
            compose.onNode(report).assertIsNotEnabled()
            compose.onNode(media).assertIsNotEnabled()
            compose.activityRule.scenario.onActivity {
                model.openReport(batchId, transfer.target)
                model.openAsset(assetId, target = transfer.target)
                model.refreshReport(batchId)
                model.refreshAsset(assetId)
            }
            compose.waitForIdle()
            assertEquals("upload", model.route.value)
            assertEquals(transfer, graph.queue.state.value!!.transfers.single { it.id == id })
        } finally {
            runBlocking { graph.media.login(a.baseUrl, requireNotNull(args.getString("qimengToken"))) }
        }
        compose.waitUntil(20_000) { model.session.value?.instanceId == a.instanceId && !model.busy.value }
        showButtons()
        compose.onNode(report).assertIsEnabled().performClick()
        compose.waitUntil(20_000) { model.report.value?.let { it.id == batchId && it.target.instanceId == a.instanceId } == true }
        compose.activityRule.scenario.onActivity { model.back() }
        showButtons()
        compose.onNode(media).assertIsEnabled().performClick()
        compose.waitUntil(20_000) { model.asset.value?.let { it.id == assetId && it.target.instanceId == a.instanceId } == true }
    }

    @Test(timeout = 240_000)
    fun readOnlyArchivePreservesOriginalAfterIndependentBackup() = runBlocking {
        archiveRoundTrip(isolatedServerGraph())
    }

    @Test(timeout = 60_000)
    fun readsDeclaredServiceVersionInAbout() {
        val graph = runBlocking { isolatedServerGraph() }
        val target = requireNotNull(graph.settings.session.value)
        val version = requireNotNull(runBlocking { graph.media.serverVersion(target) })
        val model = compose.runOnIdle { androidx.lifecycle.ViewModelProvider(compose.activity)[media.qimeng.next.LibraryViewModel::class.java] }
        compose.runOnIdle { model.selectTab(3) }
        compose.onNodeWithTag("profile-settings-list").performScrollToNode(hasText("版本信息"))
        compose.waitUntil(20_000) { compose.onAllNodesWithText("服务端 $version").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("服务端 $version").assertIsDisplayed()
        screenshot("real-server-about-version.png")
    }

    @Test(timeout = 240_000)
    fun savesIndependentAssetEditorAndPreservesDraftAfterAuthorConflict() {
        val graph = runBlocking { isolatedServerGraph() }
        val target = requireNotNull(graph.settings.session.value)
        val suffix = UUID.randomUUID().toString().take(8)
        val family = "编辑建议-$suffix"
        val library = runBlocking { graph.media.createLibrary("独立媒体编辑 $suffix", false) }
        val first = runBlocking { graph.media.createNamedAuthor(target, listOf("编辑作者甲$suffix"), UUID.randomUUID().toString()) }
        val second = runBlocking { graph.media.createNamedAuthor(target, listOf("编辑作者乙$suffix"), UUID.randomUUID().toString()) }
        fun seed(name: String, authors: List<String>): media.qimeng.next.model.Asset {
            val key = UUID.randomUUID().toString()
            val (uri, _) = writeMetadataImage(name)
            val id = runBlocking {
                val input = graph.uploads.receive(listOf(uri), intakeGroup = key).single()
                graph.uploads.renameDraft(input, name)
                graph.uploads.enqueue(library.id, "", UploadMode.ORDINARY, UploadOptions(authorIds = authors), target, listOf(input), batchKey = key)
                input
            }
            await("edit-seed", graph) { graph.queue.state.value!!.transfers.any { it.id == id && it.state == TransferState.SUCCEEDED } }
            val transfer = graph.queue.state.value!!.transfers.single { it.id == id }
            return runBlocking { graph.media.asset(requireNotNull(transfer.assetId), target) }
        }
        seed("$family 01.png", emptyList())
        val before = seed("独立编辑原名-$suffix.png", listOf(first.id, second.id))
        val model = compose.runOnIdle { androidx.lifecycle.ViewModelProvider(compose.activity)[media.qimeng.next.LibraryViewModel::class.java] }
        compose.runOnIdle { model.openAsset(before.id, target = target) }
        fun openEditor() {
            compose.waitUntil(20_000) { model.asset.value?.id == before.id && model.assetAuthors.value.size == 2 }
            compose.onNodeWithContentDescription("作者").performClick()
            compose.onNode(hasText("编辑作者与来源") and hasClickAction()).performClick()
            compose.waitUntil(20_000) { compose.onAllNodesWithTag("asset-editor-name").fetchSemanticsNodes().isNotEmpty() }
        }
        openEditor()
        editVisibleField("asset-editor-name", family)
        val suggested = "$family 02.png"
        compose.waitUntil(20_000) { compose.onAllNodesWithText(suggested).fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText(suggested).performClick()
        compose.onNodeWithTag("asset-editor-name").assertTextEquals("$family 02")
        compose.onNodeWithText("锁定").assertIsDisplayed()
        screenshot("real-server-independent-media-editor.png")
        val list = compose.onNodeWithTag("asset-editor-list")
        fun authorSource(authorId: String, value: String) {
            val tag = "asset-editor-author-source:$authorId"
            list.performScrollToNode(hasTestTag(tag))
            editVisibleField(tag, value)
        }
        authorSource(first.id, "甲手工来源")
        authorSource(second.id, "乙待保存来源")
        runBlocking { graph.media.saveAuthorSources(target, second, listOf("乙外部更新")) }
        val save = hasText("保存") and hasClickAction() and isEnabled()
        list.performScrollToNode(save)
        compose.onNode(save).performClick()
        await("edit-partial-confirmed", graph) {
            graph.media.asset(before.id, target).name == suggested &&
                graph.media.author(first.id, target).acquisitionSources == listOf("甲手工来源")
        }
        list.performScrollToIndex(0)
        val conflict = "服务端已修改，请重新读取。待保存的输入仍保留。"
        compose.waitUntil(20_000) { compose.onAllNodesWithText(conflict, substring = true).fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("已保存：文件名、${first.name}的来源").assertExists()
        val secondTag = "asset-editor-author-source:${second.id}"
        list.performScrollToNode(hasTestTag(secondTag))
        compose.onNodeWithTag(secondTag).assertTextContains("乙待保存来源")
        assertEquals(listOf("乙外部更新"), runBlocking { graph.media.author(second.id, target).acquisitionSources })

        compose.onNodeWithContentDescription("返回").performClick()
        compose.onNodeWithText("有未保存的修改").assertIsDisplayed()
        compose.onNodeWithText("继续编辑").performClick()
        compose.onNodeWithTag(secondTag).assertTextContains("乙待保存来源")
        compose.onNodeWithContentDescription("返回").performClick()
        compose.onNodeWithText("放弃修改离开").performClick()

        openEditor()
        authorSource(second.id, "乙确认来源")
        list.performScrollToNode(save)
        compose.onNode(save).performClick()
        compose.waitUntil(20_000) { model.route.value == "asset:${before.id}" }
        val result = runBlocking { graph.media.asset(before.id, target) }
        assertEquals(suggested, result.name)
        assertEquals(before.version, result.version)
        assertEquals(before.originalName, result.originalName)
        assertEquals(listOf(first.id, second.id), result.authorIds)
        assertEquals(listOf("甲手工来源"), runBlocking { graph.media.author(first.id, target).acquisitionSources })
        assertEquals(listOf("乙确认来源"), runBlocking { graph.media.author(second.id, target).acquisitionSources })
    }

    private fun editVisibleField(tag: String, text: String) {
        val field = hasTestTag(tag)
        compose.waitUntil(30_000) { compose.onAllNodes(field and isEnabled()).fetchSemanticsNodes().isNotEmpty() }
        compose.onNode(field).assertIsDisplayed().performClick()
        compose.waitUntil(30_000) { compose.onAllNodes(field and isFocused()).fetchSemanticsNodes().isNotEmpty() }
        compose.onNode(field).performTextReplacement(text)
        compose.onNode(field).assertTextEquals(text)
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

    private fun openAIAccessFromProfile() {
        compose.waitUntil(20_000) { compose.onAllNodesWithTag("navigation:me").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithTag("navigation:me").performClick()
        compose.onNodeWithTag("profile-settings-list").performScrollToNode(hasText("AI 接入"))
        compose.onNodeWithText("AI 接入").performClick()
    }

    @Test fun verifyReadOnlyAccessFromProfile() = runBlocking<Unit> {
        val graph = isolatedServerGraph()
        val label = "云端只读接入 ${UUID.randomUUID().toString().take(8)}"
        openAIAccessFromProfile()
        compose.onNodeWithText("新增 AI 接入").performClick()
        compose.onNode(hasSetTextAction()).performTextReplacement(label)
        compose.onNodeWithText("下一步").performClick()
        compose.onNodeWithText("允许单帧识图").assertDoesNotExist()
        compose.onNodeWithText("下一步").performClick()
        compose.onNodeWithText("执行动作 · 仅全库读取").assertExists()
        compose.onNodeWithText("单帧识图 · 未授权").assertExists()
        compose.onNodeWithText("授权有效期 · 30 天").assertExists()
        try {
            compose.onNodeWithText("创建接入凭据").performClick()
            compose.waitUntil(20_000) { compose.onAllNodesWithText("显示凭据").fetchSemanticsNodes().isNotEmpty() }
            compose.onNodeWithText("•••• •••• •••• ••••").assertExists()
            compose.onNodeWithText("复制凭据").performClick()
            compose.onNodeWithText("已复制").assertExists()
            compose.onNodeWithText("已保存，关闭").performClick()
            val grant = graph.aiAccess.list("active").items.single { it.policy.label == label }
            assertFalse(grant.canLookupImages)
            assertEquals(0, grant.policy.maxResearchCalls)
            val record = hasTestTag("ai-grant:${grant.id}")
            compose.onNodeWithTag("ai-access-records").performScrollToNode(record)
            compose.onNode(hasText("权限详情") and hasAnyAncestor(record)).performClick()
            compose.onNodeWithText("单帧识图 · 未授权").assertExists()
            compose.onNodeWithText("每日最多 20 个任务，每任务最多 100 次工具调用，有效 3600 秒").assertExists()
        } finally {
            graph.aiAccess.list("active").items.filter { it.policy.label == label }.forEach { graph.aiAccess.revoke(it.id) }
        }
    }

    @Test fun verifySingleFrameAccessFromProfile() = runBlocking<Unit> {
        val graph = isolatedServerGraph()
        val label = "云端单帧接入 ${UUID.randomUUID().toString().take(8)}"
        val target = graph.media.createLibrary("$label 影库", false)
        openAIAccessFromProfile()
        compose.onNodeWithText("新增 AI 接入").performClick()
        compose.onNode(hasSetTextAction()).performTextReplacement(label)
        compose.onNodeWithText("新增媒体", exact = true).performScrollTo().performClick()
        compose.onNodeWithText("研究", exact = true).performScrollTo().performClick()
        compose.onNodeWithText("下一步").performClick()
        val imageLookup = compose.onNodeWithText("允许单帧识图")
        imageLookup.assertIsNotEnabled().assertIsOff()
        compose.waitUntil(20_000) { compose.onAllNodesWithText(target.name, exact = true).fetchSemanticsNodes().isNotEmpty() }
        val library = compose.onNodeWithText(target.name, exact = true)
        library.performScrollTo().performClick()
        imageLookup.assertIsEnabled().assertIsOff().performScrollTo().performClick()
        imageLookup.assertIsOn()
        val budget = compose.onNodeWithTag("ai-access-research-budget")
        budget.performScrollTo().performTextReplacement("0")
        imageLookup.assertIsNotEnabled().assertIsOff()
        budget.performTextReplacement("2")
        imageLookup.assertIsEnabled().assertIsOff().performScrollTo().performClick()
        compose.onNodeWithText("下一步").performClick()
        val permission = "单帧识图 · 允许向已配置的识图服务发送一帧派生预览"
        compose.onNodeWithText(permission, exact = true).assertExists()
        try {
            compose.onNodeWithText("创建接入凭据").performClick()
            compose.waitUntil(20_000) { compose.onAllNodesWithText("显示凭据").fetchSemanticsNodes().isNotEmpty() }
            compose.onNodeWithText("已保存，关闭").performClick()
            val grant = graph.aiAccess.list("active").items.single { it.policy.label == label }
            assertTrue(grant.canLookupImages)
            assertTrue(grant.policy.canIngest)
            assertTrue(grant.policy.canResearch)
            assertFalse(grant.policy.canResearchAssets)
            assertFalse(grant.policy.canOrganize)
            assertEquals(listOf(target.id), grant.policy.libraryIds)
            assertEquals(2, grant.policy.maxResearchCalls)
            val record = hasTestTag("ai-grant:${grant.id}")
            compose.onNodeWithTag("ai-access-records").performScrollToNode(record)
            compose.onNode(hasText("权限详情") and hasAnyAncestor(record)).performClick()
            compose.onNode(hasText(permission) and hasAnyAncestor(record)).assertExists()
        } finally {
            graph.aiAccess.list("active").items.filter { it.policy.label == label }.forEach { graph.aiAccess.revoke(it.id) }
        }
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
    val sample = when (uri.lastPathSegment) {
        "parity-landscape.mp4" -> "qimeng-next-test-landscape.mp4"
        "parity-portrait.mp4" -> "qimeng-next-test-portrait.mp4"
        else -> "qimeng-next-test.mp4"
    }
    ParcelFileDescriptor.AutoCloseInputStream(instrument.uiAutomation.executeShellCommand("cat /data/local/tmp/$sample")).use { input ->
        requireNotNull(instrument.targetContext.contentResolver.openOutputStream(uri, "w")).use { output ->
            assertTrue("The cloud H.264 fixture must have bytes", input.copyTo(output) in 100L..8_000_000L)
        }
    }
}
