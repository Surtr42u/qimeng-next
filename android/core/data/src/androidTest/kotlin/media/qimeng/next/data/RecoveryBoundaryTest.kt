package media.qimeng.next.data

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.runBlocking
import media.qimeng.next.model.*
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.io.IOException
import java.util.UUID
import java.util.Base64
import java.util.concurrent.TimeUnit

class RecoveryBoundaryTest {
    @Test fun committedCancellationAndServerIdentitySurviveJournalReload() = runBlocking {
        val context = isolatedContext()
        val queue = QueueStore(context)
        queue.load()
        val target = Session("https://example.test/", "original-instance", "Test server")
        val transfer = Transfer("operation-001", target, "library-1", "synthetic.png", "image/png", 123, "spool-001",
            UploadMode.ORDINARY, "2026-01-01T00:00:00Z", "batch-001", "Synthetic test batch")
        queue.update { it.copy(transfers = listOf(transfer)) }
        queue.update { it.copy(transfers = it.transfers.map { record -> record.copy(state = TransferState.CANCELLED) }) }
        val recovered = QueueStore(context)
        recovered.load()
        assertEquals(TransferState.CANCELLED, recovered.state.value!!.transfers.single().state)
        assertFalse(sameTarget(recovered.state.value!!.transfers.single().target, target.copy(instanceId = "new-instance")))
    }

    @Test fun malformedJournalCannotBeReplacedWithAnEmptySuccessfulQueue() = runBlocking {
        val context = isolatedContext()
        val file = File(context.filesDir, "transfer-journal.json")
        file.writeText("{broken")
        val queue = QueueStore(context)
        try { queue.load(); fail("Corrupt journal accepted") } catch (_: Exception) { }
        assertNull(queue.state.value)
        assertNotNull(queue.error.value)
        assertEquals("{broken", file.readText())
    }

    @Test fun backupOnlyAtomicFileWindowRecoversCommittedRecords() = runBlocking {
        val context = isolatedContext()
        val queue = QueueStore(context)
        queue.load()
        queue.update { it.copy(intakeFailures = listOf(IntakeFailure("fixture", "synthetic.png", "synthetic failure"))) }
        val base = File(context.filesDir, "transfer-journal.json")
        assertTrue(base.renameTo(File(base.path + ".bak")))
        val recovered = QueueStore(context)
        recovered.load()
        assertEquals("fixture", recovered.state.value!!.intakeFailures.single().id)
    }

    @Test fun credentialsStayOnTheirOriginAndAreNotFollowedThroughRedirects() {
        val server = MockWebServer()
        val other = MockWebServer()
        server.start(); other.start()
        try {
            val settings = SecureSettings(isolatedContext())
            val session = Session(server.url("/").toString(), "test-instance", "Synthetic server")
            settings.saveSession(session, "synthetic-test-token")
            val client = ApiProvider(settings).client(session)
            server.enqueue(MockResponse().setResponseCode(302).setHeader("Location", other.url("/capture")))
            client.newCall(Request.Builder().url(server.url("/media")).build()).execute().use { assertEquals(302, it.code) }
            assertEquals("Bearer synthetic-test-token", server.takeRequest(2, TimeUnit.SECONDS)!!.getHeader("Authorization"))
            assertNull(other.takeRequest(150, TimeUnit.MILLISECONDS))
            assertThrows(IOException::class.java) { client.newCall(Request.Builder().url(other.url("/media")).build()).execute().close() }
            assertNull(other.takeRequest(150, TimeUnit.MILLISECONDS))
            settings.saveSession(session.copy(instanceId = "replacement-instance"), "synthetic-replacement-token")
            assertThrows(ApiFailure::class.java) { client.newCall(Request.Builder().url(server.url("/media")).build()).execute().close() }
            settings.clearSession()
            assertThrows(ApiFailure::class.java) { client.newCall(Request.Builder().url(server.url("/media")).build()).execute().close() }
        } finally { server.shutdown(); other.shutdown() }
    }

    @Test fun pinnedLeafAndHostnameProtectCredentialsAcrossRuntimeRestart() {
        val certificate = HeldCertificate.Builder().commonName("Synthetic local server").addSubjectAlternativeName("localhost").build()
        val tls = HandshakeCertificates.Builder().heldCertificate(certificate).build()
        val server = MockWebServer().apply { useHttps(tls.sslSocketFactory(), false); start() }
        try {
            val settings = SecureSettings(isolatedContext())
            val session = Session(server.url("/").toString(), "synthetic-pinned-instance", "Synthetic")
            val encoded = Base64.getEncoder().encodeToString(certificate.certificate.encoded)
            settings.saveSession(session, "synthetic-owner-session", encoded)
            val provider = ApiProvider(settings)
            val original = provider.client(session)
            server.enqueue(MockResponse().setBody("ok"))
            original.newCall(Request.Builder().url(server.url("/media")).build()).execute().use { assertEquals(200, it.code) }
            assertEquals("Bearer synthetic-owner-session", server.takeRequest(2, TimeUnit.SECONDS)!!.getHeader("Authorization"))

            val replacement = HeldCertificate.Builder().commonName("Synthetic replacement").addSubjectAlternativeName("localhost").build()
            settings.saveSession(session, "synthetic-new-session", Base64.getEncoder().encodeToString(replacement.certificate.encoded))
            assertThrows(ApiFailure::class.java) { original.newCall(Request.Builder().url(server.url("/media")).build()).execute().close() }
            assertNull(server.takeRequest(100, TimeUnit.MILLISECONDS))
            assertThrows(IOException::class.java) { provider.client(session).newCall(Request.Builder().url(server.url("/media")).build()).execute().close() }
            assertNull(server.takeRequest(150, TimeUnit.MILLISECONDS)?.getHeader("Authorization"))

            // Same pinned certificate is insufficient when the URL host does not match its SAN.
            val wrongHost = session.copy(baseUrl = server.url("/").newBuilder().host("127.0.0.1").build().toString())
            settings.saveSession(wrongHost, "synthetic-not-for-wrong-host", encoded)
            assertThrows(IOException::class.java) { provider.client(wrongHost).newCall(Request.Builder().url(wrongHost.baseUrl).build()).execute().close() }
            assertNull(server.takeRequest(150, TimeUnit.MILLISECONDS)?.getHeader("Authorization"))
        } finally { server.shutdown() }
    }

    private fun isolatedContext(): Context {
        val base = InstrumentationRegistry.getInstrumentation().targetContext
        val suffix = UUID.randomUUID().toString()
        val directory = File(base.cacheDir, "boundary-$suffix").apply { mkdirs() }
        return object : ContextWrapper(base) {
            override fun getFilesDir(): File = directory
            override fun getSharedPreferences(name: String, mode: Int): SharedPreferences = base.getSharedPreferences("$suffix-$name", mode)
        }
    }
}
