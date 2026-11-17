package media.qimeng.next.test

import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.runBlocking
import media.qimeng.next.MainActivity
import media.qimeng.next.QimengApplication
import media.qimeng.next.runtime.RuntimePhase
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test

/** Separate cloud invocation after the client suite, with a fresh synthetic app data directory. */
class EmbeddedRuntimeTest {
    @get:Rule val compose = createAndroidComposeRule<MainActivity>()

    @Test(timeout = 180_000)
    fun realPackagedServerUsesPinnedTlsAndPreservesInstanceAcrossRestart() = runBlocking {
        require(InstrumentationRegistry.getArguments().getString("qimengEmbedded") == "true") { "Explicit embedded cloud runner argument required" }
        val application = compose.activity.application as QimengApplication
        val runtime = application.runtime
        val graph = application.graph
        assertTrue("This suite requires the embedded APK", runtime.available)
        try {
            val first = runtime.start()
            assertEquals("https://127.0.0.1:18431/", first.baseUrl)
            assertTrue(first.certificateDerBase64.isNotBlank())
            graph.media.login(first.baseUrl, first.bootstrapToken, first.certificateDerBase64, first.instanceId)
            runtime.confirmInstance(first.instanceId)
            val library = graph.media.createLibrary("Embedded synthetic cloud library", false)
            assertTrue(graph.media.libraries().any { it.id == library.id })

            runtime.stop()
            assertEquals(RuntimePhase.STOPPED, runtime.status.value.phase)
            val second = runtime.start()
            assertEquals(first.instanceId, second.instanceId)
            assertNotEquals("Every native restart must replace its ephemeral certificate", first.certificateDerBase64, second.certificateDerBase64)
            graph.media.login(second.baseUrl, second.bootstrapToken, second.certificateDerBase64, second.instanceId)
            runtime.confirmInstance(second.instanceId)
            assertTrue("Server persistence must survive process restart", graph.media.libraries().any { it.id == library.id })
        } finally { runtime.stop() }
    }
}
