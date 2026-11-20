package media.qimeng.next.test

import android.graphics.Bitmap
import android.graphics.Color
import android.net.Uri
import android.os.ParcelFileDescriptor
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isEnabled
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextReplacement
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import media.qimeng.next.MainActivity
import media.qimeng.next.QimengApplication
import media.qimeng.next.data.DataGraph
import media.qimeng.next.model.Asset
import media.qimeng.next.runtime.RuntimePhase
import media.qimeng.next.embedded.EmbeddedDebugDiagnostics
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.model.UploadMode
import media.qimeng.next.model.TransferState
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test

/** Separate cloud invocation after the client suite, with a fresh synthetic app data directory. */
class EmbeddedRuntimeTest {
    @get:Rule val compose = createAndroidComposeRule<MainActivity>()

    @Test(timeout = 300_000)
    fun realPackagedServerUsesPinnedTlsAndPreservesInstanceAcrossRestart() = runBlocking {
        require(InstrumentationRegistry.getArguments().getString("qimengEmbedded") == "true") { "Explicit embedded cloud runner argument required" }
        val application = compose.activity.application as QimengApplication
        val runtime = application.runtime
        val graph = application.graph
        assertTrue("This suite requires the embedded APK", runtime.available)
        EmbeddedDebugDiagnostics.enable()
        try {
            val first = runtime.start()
            assertEquals("https://127.0.0.1:18431/", first.baseUrl)
            assertTrue(first.certificateDerBase64.isNotBlank())
            application.localConnection.connect(forceLogin = true)
            // Use the same trusted spelling as the host. Android's public app-data alias can differ.
            val root = java.io.File(application.filesDir.canonicalFile, "embedded-v1/media/synthetic-registration")
            assertTrue("Create the existing synthetic media folder", root.mkdir() || root.isDirectory)
            val library = graph.media.registerLibrary(graph.settings.session.value!!, "Embedded synthetic cloud library",
                root.absolutePath, false, "synthetic-cloud-register-library")
            assertTrue(graph.media.libraryPage().items.any { it.id == library.id })
            assertTrue("Owner root must round trip", graph.media.libraryRoot(graph.settings.session.value!!, library.id) == root.absolutePath)

            runtime.stop()
            assertEquals(RuntimePhase.STOPPED, runtime.status.value.phase)
            assertFalse(runtime.shouldRestore)
            val savedLocal = requireNotNull(graph.settings.session.value)
            assertFalse("Saved session must not bypass host readiness", application.transferConnectionReady(savedLocal))
            assertTrue("Foreign loopback session is not gated as embedded",
                application.transferConnectionReady(savedLocal.copy(instanceId = "different-instance")))
            graph.uploads.schedule()
            assertNull("An idle/stopped host must not masquerade as a failed transfer", graph.queue.error.value)
            application.localConnection.restoreSelected()
            assertEquals("Returning to the app must respect an explicit stop", RuntimePhase.STOPPED, runtime.status.value.phase)
            val second = runtime.start()
            assertEquals(first.instanceId, second.instanceId)
            assertNotEquals("Every native restart must replace its ephemeral certificate", first.certificateDerBase64, second.certificateDerBase64)
            // A READY transition while the same Activity is alive must renew the cached pin without onStart.
            withTimeout(15_000) { application.localConnection.admitted.first { it?.instanceId == first.instanceId } }
            assertEquals(second.certificateDerBase64, graph.settings.certificateFor(graph.settings.session.value!!))
            assertTrue("Fresh pinned session reopens admission without replacing the saved library identity",
                application.transferConnectionReady(savedLocal))
            assertTrue("Server persistence must survive process restart", graph.media.libraryPage().items.any { it.id == library.id })
            runtime.configureAddress("http://127.0.0.1:18432")
            application.localConnection.connect(forceLogin = true)
            assertEquals("https://127.0.0.1:18432/", graph.settings.session.value!!.baseUrl)
            assertEquals(first.instanceId, graph.settings.session.value!!.instanceId)
            assertTrue("An owned port change preserves the same database", graph.media.libraryPage().items.any { it.id == library.id })

            val source = Uri.parse("content://media.qimeng.next.test.fixture/android-synthetic.png")
            val bitmap = Bitmap.createBitmap(40, 30, Bitmap.Config.ARGB_8888).apply { eraseColor(Color.rgb(72, 112, 190)) }
            try { compose.activity.contentResolver.openOutputStream(source, "w")!!.use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) } }
            finally { bitmap.recycle() }
            val input = graph.uploads.receive(listOf(source)).single()
            graph.uploads.enqueue(library.id, "", UploadMode.ORDINARY, inputIds = listOf(input))
            val transfer = withTimeout(90_000) {
                while (graph.queue.state.value!!.transfers.single { it.id == input }.state != TransferState.SUCCEEDED) {
                    val item = graph.queue.state.value!!.transfers.single { it.id == input }
                    check(item.state != TransferState.FAILED) { "Synthetic native upload failed: ${item.message}" }
                    delay(300)
                }
                graph.queue.state.value!!.transfers.single { it.id == input }
            }
            val target = graph.settings.session.value!!
            val receipt = graph.media.report(requireNotNull(transfer.batchId)).items.single { it.id == input }
            val assetId = requireNotNull(transfer.assetId) { "Ordinary upload missing asset identity; input=${receipt.state}; transfer=${transfer.message}" }
            val asset = graph.media.asset(assetId)
            val page = graph.media.assets(AssetQuery(libraryId = library.id))
            val projection = graph.media.library(target, library.id)
            assertTrue("Native upload membership; targetMatches=${media.qimeng.next.model.sameTarget(transfer.target,target)}; " +
                "receipt=${receipt.state}; assetLibraryMatches=${asset.libraryId == library.id}; " +
                "enabled=${projection.enabled}; indexCount=${projection.fileCount}; matched=${page.totalMatched}; " +
                "listed=${page.items.size}; originalPresent=${java.io.File(root,asset.relativePath).isFile}",
                asset.libraryId == library.id && receipt.assetId == assetId && page.items.any { it.id == assetId })

            // The actual stopped host must preserve nonempty journal work and replay it only after re-admission.
            val viewsBeforeStop = graph.analytics.report("7d").overview.totals.views
            runtime.stop()
            val visitSession = java.util.UUID.randomUUID().toString()
            graph.recordEvent(asset, "view", visitSession)
            val queuedVisit = withTimeout(10_000) {
                graph.queue.state.first { it?.events?.any { event -> event.sessionId == visitSession } == true }!!
                    .events.single { it.sessionId == visitSession }
            }
            assertFalse(application.transferConnectionReady(target))
            assertNull(queuedVisit.issue)
            assertNull(graph.queue.error.value)
            val restarted = runtime.start()
            assertEquals(target.instanceId, restarted.instanceId)
            withTimeout(20_000) { application.localConnection.admitted.first { it?.baseUrl == target.baseUrl } }
            withTimeout(30_000) { graph.queue.state.first { it != null && it.events.none { event -> event.id == queuedVisit.id } } }
            assertTrue("Recovered browsing event reached the same physical library",
                graph.media.assets(AssetQuery(source = "history")).items.any { it.id == assetId && it.lastViewedAt != null })
            assertEquals("The pending visit was counted once", viewsBeforeStop + 1,
                graph.analytics.report("7d").overview.totals.views)

            shell("dumpsys battery unplug")
            shell("dumpsys battery set status 3")
            awaitPower(plugged = false)
            runtime.scans.setChargeOnly(true)
            runtime.scans.request(first.instanceId, library.id, library.revision)
            delay(1_500)
            assertNull("A pending scan must not be submitted while unplugged", runtime.scans.state.value.requests.single().jobId)
            shell("dumpsys battery set ac 1")
            shell("dumpsys battery set status 2")
            awaitPower(plugged = true)
            val scanId = withTimeout(60_000) {
                while (runtime.scans.state.value.requests.single().jobId == null) {
                    val pending = runtime.scans.state.value.requests.single()
                    check(pending.state != "failed") { "Synthetic native scan submission failed: ${pending.message}" }
                    delay(300)
                }
                runtime.scans.state.value.requests.single().jobId!!
            }
            shell("dumpsys battery unplug")
            withTimeout(60_000) {
                while (graph.scans.get(scanId).state in setOf("queued", "running")) delay(300)
            }
            assertEquals("Unplugging does not cancel a scan already accepted by Go", "completed", graph.scans.get(scanId).state)
            compose.activityRule.scenario.onActivity {
                androidx.lifecycle.ViewModelProvider(it)[media.qimeng.next.LibraryViewModel::class.java].navigate("libraries")
            }
            compose.waitUntil(30_000) { compose.onAllNodesWithText("1 个文件").fetchSemanticsNodes().isNotEmpty() }
            media.qimeng.next.testsupport.captureCloudScreenshot("embedded-library-registration.png")
            compose.onNodeWithText("删除").performClick()
            compose.onNodeWithText("该库 1 个文件的索引将被清除，磁盘文件不受影响。").assertExists()
            compose.onAllNodesWithText("删除").let { it[it.fetchSemanticsNodes().lastIndex].performClick() }
            withTimeout(20_000) { while (graph.media.libraryPage().items.any { it.id == library.id }) delay(200) }
            assertTrue("Unregister keeps the original on disk", java.io.File(root, asset.relativePath).isFile)

            val video = uploadTargetVideo(graph)
            compose.activityRule.scenario.onActivity {
                androidx.lifecycle.ViewModelProvider(it)[media.qimeng.next.LibraryViewModel::class.java].navigate("maintenance")
            }
            compose.waitUntil(20_000) { compose.onAllNodes(hasText("设备领取") and isEnabled()).fetchSemanticsNodes().isNotEmpty() }
            compose.onNodeWithText("设备领取").performClick()
            compose.onNodeWithText("本机影库标识 ${target.instanceId}").assertExists()
            assertCopied("本机影库标识", target.instanceId)
            compose.onNodeWithText("选择本机目标视频").performScrollTo().performClick()
            fun search(query: String) {
                compose.waitUntil(20_000) { compose.onAllNodes(hasText("搜索视频") and isEnabled()).fetchSemanticsNodes().isNotEmpty() }
                compose.onNodeWithTag("device-target-video-search").performScrollTo().performTextReplacement(query)
                compose.onNodeWithText("搜索视频").performScrollTo().performClick()
            }
            search("unmatched-local-mark-fixture")
            compose.waitUntil(20_000) { compose.onAllNodesWithText("没有符合条件的本机视频").fetchSemanticsNodes().isNotEmpty() }
            search(video.name)
            val result = hasText(video.name) and SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Button)
            compose.waitUntil(20_000) { compose.onAllNodes(result and isEnabled()).fetchSemanticsNodes().isNotEmpty() }
            compose.onNode(result).performScrollTo().performClick()
            compose.waitUntil(20_000) { compose.onAllNodesWithContentDescription("复制目标视频 ID").fetchSemanticsNodes().isNotEmpty() }
            assertCopied("目标视频 ID", video.id)
            assertCopied("目标内容版本", video.version.toString())
            compose.onNodeWithText("收起目标视频").performScrollTo().performClick()
            compose.onNodeWithText("生成本机设备令牌").performScrollTo().performClick()
            compose.waitUntil(20_000) { graph.deviceDelivery.view().configured &&
                compose.onAllNodes(hasTestTag("device-background") and isEnabled()).fetchSemanticsNodes().isNotEmpty() }
            val initial = graph.deviceDelivery.view()
            assertFalse(initial.background)
            assertFalse(initial.replaceAuthorProfiles)
            compose.onNodeWithTag("device-background").performScrollTo().performClick()
            compose.onNodeWithTag("device-replace-author-profiles").performScrollTo().performClick()
            val draft = graph.deviceDelivery.view()
            assertFalse("Editing the form must not enable the worker", draft.background)
            assertFalse("The unfinished form must preserve the saved policy", draft.replaceAuthorProfiles)
            compose.onNodeWithText("保存设置").performScrollTo().performClick()
            compose.waitUntil(20_000) { graph.deviceDelivery.view().let { it.background && it.replaceAuthorProfiles && it.destination == target.instanceId } }
            compose.waitUntil(20_000) { compose.onAllNodes(hasText("清除设备令牌") and isEnabled()).fetchSemanticsNodes().isNotEmpty() }
            compose.onNodeWithText("清除设备令牌").performScrollTo().performClick()
            compose.waitUntil(20_000) { graph.deviceDelivery.view().let { !it.configured && !it.background } }
        } catch (failure: Throwable) {
            throw AssertionError("Synthetic native runtime failure: ${runtime.status.value}; native output:\n${EmbeddedDebugDiagnostics.snapshot()}", failure)
        } finally {
            shell("dumpsys battery reset")
            runtime.stop()
            runtime.configureAddress("https://127.0.0.1:18431/")
            EmbeddedDebugDiagnostics.disable()
        }
    }

    private suspend fun uploadTargetVideo(graph: DataGraph): Asset {
        val library = graph.media.createLibrary("本机标记接收样本", false)
        val uri = Uri.parse("content://media.qimeng.next.test.fixture/android-synthetic.mp4")
        copyCloudVideo(uri)
        val id = graph.uploads.receive(listOf(uri)).single()
        graph.uploads.enqueue(library.id, "", UploadMode.ORDINARY, inputIds = listOf(id))
        val assetId = withTimeout(60_000) {
            while (graph.queue.state.value!!.transfers.single { it.id == id }.state != TransferState.SUCCEEDED) {
                val item = graph.queue.state.value!!.transfers.single { it.id == id }
                check(item.state != TransferState.FAILED) { "Target video upload failed: ${item.message}" }
                delay(250)
            }
            requireNotNull(graph.queue.state.value!!.transfers.single { it.id == id }.assetId)
        }
        return graph.media.asset(assetId)
    }

    private fun assertCopied(label: String, expected: String) {
        var focused = false
        var observed: String? = null
        var stage = "await-window-focus"
        try {
            compose.waitUntil(15_000) {
                compose.activityRule.scenario.onActivity { focused = it.hasWindowFocus() }
                focused
            }
            stage = "copy-click"
            compose.onNodeWithContentDescription("复制$label").performScrollTo().performClick()
            stage = "clipboard-read"
            compose.waitUntil(15_000) {
                compose.activityRule.scenario.onActivity { activity ->
                    focused = activity.hasWindowFocus()
                    if (focused) {
                        val clipboard = requireNotNull(activity.getSystemService(android.content.ClipboardManager::class.java))
                        observed = clipboard.primaryClip?.getItemAt(0)?.text?.toString()
                    }
                }
                focused && observed == expected
            }
        } catch (failure: androidx.compose.ui.test.ComposeTimeoutException) {
            var window = "unavailable"
            runCatching {
                compose.activityRule.scenario.onActivity { activity ->
                    val decor = activity.window.decorView
                    window = "lifecycle=${activity.lifecycle.currentState}, activityFocus=${activity.hasWindowFocus()}, " +
                        "decorFocus=${decor.hasWindowFocus()}, shown=${decor.isShown}, " +
                        "viewFocus=${activity.currentFocus?.javaClass?.simpleName}"
                }
            }.exceptionOrNull()?.let(failure::addSuppressed)
            val activeWindow = runCatching {
                InstrumentationRegistry.getInstrumentation().uiAutomation.rootInActiveWindow?.let {
                    // Describe the foreground owner only; never include node text or clipboard contents.
                    "package=${it.packageName}, class=${it.className}"
                } ?: "no-accessibility-root"
            }.getOrElse { "unavailable:${it.javaClass.simpleName}" }
            runCatching { media.qimeng.next.testsupport.captureCloudScreenshot("embedded-copy-failure.png") }
                .exceptionOrNull()?.let(failure::addSuppressed)
            throw AssertionError("$label 复制检查超时，阶段=$stage，窗口焦点=$focused，" +
                "系统已提供文本=${observed != null}; $window; activeWindow=$activeWindow", failure)
        }
    }

    private suspend fun awaitPower(plugged: Boolean) = withTimeout(12_000) {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        while (true) {
            val battery = context.registerReceiver(null, android.content.IntentFilter(android.content.Intent.ACTION_BATTERY_CHANGED))
            val supply = battery?.getIntExtra(android.os.BatteryManager.EXTRA_PLUGGED, 0)
            val status = battery?.getIntExtra(android.os.BatteryManager.EXTRA_STATUS, 0)
            if (supply != null && (supply != 0) == plugged && status ==
                (if (plugged) android.os.BatteryManager.BATTERY_STATUS_CHARGING else android.os.BatteryManager.BATTERY_STATUS_DISCHARGING)) break
            delay(100)
        }
    }

    private fun shell(command: String) {
        ParcelFileDescriptor.AutoCloseInputStream(InstrumentationRegistry.getInstrumentation().uiAutomation.executeShellCommand(command)).use { input ->
            val buffer = ByteArray(512)
            while (input.read(buffer) >= 0) Unit
        }
    }
}
