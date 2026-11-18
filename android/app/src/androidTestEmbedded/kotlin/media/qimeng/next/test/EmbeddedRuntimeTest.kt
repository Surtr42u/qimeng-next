package media.qimeng.next.test

import android.graphics.Bitmap
import android.graphics.Color
import android.net.Uri
import android.os.ParcelFileDescriptor
import androidx.compose.ui.test.junit4.createAndroidComposeRule
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
            val library = graph.media.createLibrary("Embedded synthetic cloud library", false)
            assertTrue(graph.media.libraries().any { it.id == library.id })

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
            assertTrue("Server persistence must survive process restart", graph.media.libraries().any { it.id == library.id })
            runtime.configureAddress("http://127.0.0.1:18432")
            application.localConnection.connect(forceLogin = true)
            assertEquals("https://127.0.0.1:18432/", graph.settings.session.value!!.baseUrl)
            assertEquals(first.instanceId, graph.settings.session.value!!.instanceId)
            assertTrue("An owned port change preserves the same database", graph.media.libraries().any { it.id == library.id })

            val source = Uri.parse("content://media.qimeng.next.test.fixture/android-synthetic.png")
            val bitmap = Bitmap.createBitmap(40, 30, Bitmap.Config.ARGB_8888).apply { eraseColor(Color.rgb(72, 112, 190)) }
            try { compose.activity.contentResolver.openOutputStream(source, "w")!!.use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) } }
            finally { bitmap.recycle() }
            graph.uploads.receive(listOf(source))
            graph.uploads.enqueue(library.id, "", UploadMode.ORDINARY)
            withTimeout(90_000) {
                while (graph.queue.state.value!!.transfers.none { it.state == TransferState.SUCCEEDED }) {
                    check(graph.queue.state.value!!.transfers.none { it.state == TransferState.FAILED }) { "Synthetic native upload failed" }
                    delay(300)
                }
            }
            assertEquals(1, graph.media.assets(AssetQuery(libraryId = library.id)).items.size)

            shell("dumpsys battery unplug")
            runtime.scans.setChargeOnly(true)
            runtime.scans.request(first.instanceId, library.id, library.revision)
            delay(1_500)
            assertNull("A pending scan must not be submitted while unplugged", runtime.scans.state.value.requests.single().jobId)
            shell("dumpsys battery set ac 1")
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
        } catch (failure: Throwable) {
            throw AssertionError("Synthetic native runtime failure: ${runtime.status.value}; native output:\n${EmbeddedDebugDiagnostics.snapshot()}", failure)
        } finally {
            shell("dumpsys battery reset")
            runtime.stop()
            runtime.configureAddress("https://127.0.0.1:18431/")
            EmbeddedDebugDiagnostics.disable()
        }
    }

    private fun shell(command: String) {
        ParcelFileDescriptor.AutoCloseInputStream(InstrumentationRegistry.getInstrumentation().uiAutomation.executeShellCommand(command)).use { input ->
            val buffer = ByteArray(512)
            while (input.read(buffer) >= 0) Unit
        }
    }
}
