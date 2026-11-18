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
    @Test fun ordinarySuccessWithoutAssetReconcilesOriginalUploadAfterRestart() = runBlocking {
        val context = isolatedContext()
        val queue = QueueStore(context)
        queue.load()
        val target = Session("https://example.test/", "archive-instance", "Synthetic")
        val item = Transfer("ordinary", target, "library", "recover.png", "image/png", 54, "spool-already-cleaned",
            UploadMode.ORDINARY, null, "batch", "Synthetic", state = TransferState.SUCCEEDED, uploadId = "original-upload", offset = 54)
        val intelligent = item.copy(id = "intelligent", mode = UploadMode.INTELLIGENT)
        val published = item.copy(id = "published", assetId = "original-asset")
        queue.update { it.copy(transfers = listOf(item, intelligent, published)) }
        val restored = QueueStore(context)
        restored.load()
        UploadRepository(context, restored, SecureSettings(context)).recoverInterruptedIntake()
        val entries = requireNotNull(restored.state.value).transfers
        assertEquals(TransferState.QUEUED, entries[0].state)
        assertEquals("original-upload", entries[0].uploadId)
        assertEquals(54L, entries[0].offset)
        assertEquals(listOf(intelligent, published), entries.drop(1))
    }

    @Test fun authenticatedEndpointMoveKeepsOperationIdentityAndDoesNotMergeAnotherAddress() = runBlocking {
        val queue = QueueStore(isolatedContext())
        queue.load()
        val old = Session("https://127.0.0.1:18431/", "owned-instance", "Local")
        val next = old.copy(baseUrl = "https://127.0.0.1:18432/")
        val unrelated = old.copy(baseUrl = "https://unrelated.example/")
        val item = Transfer("operation-owned", old, "library", "synthetic.png", "image/png", 12, "spool-owned", UploadMode.ORDINARY,
            null, "batch-owned", "Synthetic", uploadId = "upload-owned", state = TransferState.WAITING)
        val event = PendingEvent("event-owned", old, "asset-owned", "view", 0, 1, "session-owned", occurredAt = "2026-01-01T00:00:00Z")
        queue.update { it.copy(transfers = listOf(item, item.copy(id = "operation-remote", target = unrelated)), events = listOf(event)) }
        queue.rebindEndpoint(next, setOf(old.baseUrl))
        val result = queue.state.value!!
        assertEquals(next, result.transfers[0].target)
        assertEquals(TransferState.QUEUED, result.transfers[0].state)
        assertEquals("upload-owned", result.transfers[0].uploadId)
        assertEquals(unrelated, result.transfers[1].target)
        assertEquals(TransferState.WAITING, result.transfers[1].state)
        assertEquals(event.copy(target = next), result.events.single())
    }

    @Test fun originalManifestKeepsUnreadInputsAndCancellationReceiptAfterRestart() = runBlocking {
        val context = isolatedContext()
        val queue = QueueStore(context)
        queue.load()
        val target = Session("https://example.test/", "manifest-instance", "Synthetic")
        val inputs = listOf(OriginalInput("input-known-001", "original.png", 32, "a".repeat(64), "known"),
            OriginalInput("input-unavailable-002"))
        val batch = TransferBatch("batch-selection-001", target, "library", UploadMode.ORDINARY, "Two original inputs", inputs,
            serverId = "server-batch", manifestFingerprint = "b".repeat(64))
        val transfer = Transfer(inputs[0].id, target, "library", "renamed.png", "image/png", 32, "spool-synthetic", UploadMode.ORDINARY,
            null, batch.key, batch.label, state = TransferState.CANCELLED, batchId = batch.serverId, cancelConfirmed = true)
        queue.update { it.copy(batches = listOf(batch), transfers = listOf(transfer)) }
        val recovered = QueueStore(context)
        recovered.load()
        val restored = recovered.state.value!!
        assertEquals(inputs, restored.batches.single().inputs)
        assertNull(restored.batches.single().inputs[1].originalFileName)
        assertEquals("unavailable", restored.batches.single().inputs[1].nameState)
        assertEquals(batch.manifestFingerprint, restored.batches.single().manifestFingerprint)
        assertTrue(restored.transfers.single().cancelConfirmed)
        assertNull(restored.transfers.single().sourceMtime)
    }

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
            assertThrows(IOException::class.java) { original.newCall(Request.Builder().url(server.url("/media")).build()).execute().close() }
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
