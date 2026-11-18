package media.qimeng.next.test

import android.graphics.Bitmap
import android.graphics.Color
import android.net.Uri
import android.os.ParcelFileDescriptor
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.performClick
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import media.qimeng.next.MainActivity
import media.qimeng.next.QimengApplication
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
            application.localConnection.restoreSelected()
            assertEquals("Returning to the app must respect an explicit stop", RuntimePhase.STOPPED, runtime.status.value.phase)
            val second = runtime.start()
            assertEquals(first.instanceId, second.instanceId)
            assertNotEquals("Every native restart must replace its ephemeral certificate", first.certificateDerBase64, second.certificateDerBase64)
            // Share the app coordinator with automatic pin refresh, so the test cannot race two logins.
            application.localConnection.connect(forceLogin = false)
            assertEquals(second.certificateDerBase64, graph.settings.certificateFor(graph.settings.session.value!!))
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
        } catch (failure: Throwable) {
            throw AssertionError("Synthetic native runtime failure: ${runtime.status.value}; native output:\n${EmbeddedDebugDiagnostics.snapshot()}", failure)
        } finally {
            shell("dumpsys battery reset")
            runtime.stop()
            runtime.configureAddress("https://127.0.0.1:18431/")
            EmbeddedDebugDiagnostics.disable()
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
