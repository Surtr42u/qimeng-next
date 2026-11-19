package media.qimeng.next.data

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import media.qimeng.next.model.*
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.RecordedRequest
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.io.IOException
import java.util.UUID
import java.util.Base64
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

class RecoveryBoundaryTest {
    @Test fun libraryReadsPreserveTheServersOrderScoresCursorsAndNames() = runBlocking {
        val server = MockWebServer()
        server.start()
        try {
            val settings = SecureSettings(isolatedContext())
            val session = Session(server.url("/").toString(), "authority-fixture", "Synthetic server")
            settings.saveSession(session, "synthetic-authority-credential")
            val provider = ApiProvider(settings)
            val media = MediaRepository(settings, provider)
            fun reply(json: String) = server.enqueue(MockResponse().setHeader("Content-Type", "application/json").setBody(json))
            val first = syntheticAsset("z")
            val second = syntheticAsset("a")
            reply("""{"items":[$first,$second],"snapshotId":"stable","nextCursor":"opaque-recommendation"}""")
            val recommendations = media.assets(AssetQuery(source = "recommendations", seed = 91))
            assertEquals(listOf("z", "a"), recommendations.items.map { it.id })
            assertEquals("opaque-recommendation", recommendations.nextCursor)
            val recommendationRequest = server.takeRequest(3, TimeUnit.SECONDS)!!.requestUrl!!
            assertEquals("/api/v1/recommendations", recommendationRequest.encodedPath)
            assertEquals("91", recommendationRequest.queryParameter("seed"))

            reply("""{"items":[{"asset":$first,"score":712},{"asset":$second,"score":641}],"nextCursor":"opaque-ranking"}""")
            val rankings = media.assets(AssetQuery(source = "rankings", period = "all"))
            assertEquals(listOf(712L, 641L), rankings.items.map { it.rankingScore })
            assertEquals(listOf("z", "a"), rankings.items.map { it.id })
            assertEquals("opaque-ranking", rankings.nextCursor)
            assertEquals("/api/v1/rankings", server.takeRequest(3, TimeUnit.SECONDS)!!.requestUrl!!.encodedPath)

            reply("""{"policyVersion":"fixture","items":[{"family":"Z","suggestedName":"chosen-by-server","numberClass":"image","workNumber":73}],"truncated":false}""")
            reply("""{"policyVersion":"fixture","libraryId":"library","directory":"covers","family":"Z","numberClass":"image",
                "nextWorkNumber":73,"width":4,"separator":"-","namespaceFingerprint":"scope","reserved":false}""")
            reply("""{"policyVersion":"fixture","namespaceFingerprint":"scope","reserved":false,"items":[{"fileName":"published.png",
                "relativePath":"covers/served-name-0073.png","numberClass":"image","workNumber":73}]}""")
            val naming = NamingRepository(settings, provider).inspect(NamingRequest("library", "covers", "Z", null,
                "image", "", listOf(NamingFile("single", "phone-original.png"))))
            assertEquals("chosen-by-server", naming.suggestions.single().example)
            assertEquals(73, naming.position!!.nextWork)
            assertEquals(listOf("covers/served-name-0073.png"), naming.preview!!.names)
            val namingPaths = List(3) { server.takeRequest(3, TimeUnit.SECONDS)!!.requestUrl!!.encodedPath.substringAfter("/naming/") }
            assertEquals(listOf("suggestions", "context", "preview"), namingPaths)
        } finally {
            server.shutdown()
        }
    }

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

    @Test fun transferReportAndMediaNeverReachTheOtherServerWhenTheConnectionChanges() = runBlocking {
        val origin = MockWebServer()
        val other = MockWebServer()
        val arrived = CountDownLatch(2)
        val release = CountDownLatch(1)
        val batchId = "origin-batch"
        val assetId = "origin-asset"
        origin.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                arrived.countDown()
                if (!release.await(10, TimeUnit.SECONDS)) return MockResponse().setResponseCode(503)
                val json = when (request.path) {
                    "/api/v1/assets/$assetId" -> syntheticAsset(assetId)
                    "/api/v1/batches/$batchId" -> """{"batch":{"id":"$batchId","libraryId":"library","mode":"ordinary","label":"origin",
                        "revision":2,"state":"completed","manifestRevision":2,"manifestState":"sealed","expectedInputs":0,
                        "manifestFingerprint":"fixture","backupState":"not_covered","cleanupState":"retained","researchCalls":0,
                        "createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"},"inputs":[],"items":[],"plans":[]}"""
                    else -> return MockResponse().setResponseCode(404)
                }
                return MockResponse().setHeader("Content-Type", "application/json").setBody(json)
            }
        }
        other.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest) = MockResponse().setResponseCode(404)
        }
        origin.start()
        other.start()
        try {
            val settings = SecureSettings(isolatedContext())
            val media = MediaRepository(settings, ApiProvider(settings))
            val a = Session(origin.url("/").toString(), "origin", "A")
            val b = Session(other.url("/").toString(), "other", "B")
            val task = Transfer("selected", a, "library", "good.png", "image/png", 100L, "snapshot", UploadMode.ORDINARY, null,
                "local-batch", "Selection", state = TransferState.SUCCEEDED, assetId = assetId, batchId = batchId)
            suspend fun report() = media.report(requireNotNull(task.batchId), task.target)
            suspend fun asset() = media.asset(requireNotNull(task.assetId), task.target)
            settings.saveSession(a, "synthetic-a")
            val pendingReport = async(Dispatchers.IO) { runCatching { report() }.exceptionOrNull() }
            val pendingAsset = async(Dispatchers.IO) { runCatching { asset() }.exceptionOrNull() }
            assertTrue(arrived.await(8, TimeUnit.SECONDS))
            settings.saveSession(b, "synthetic-b")
            release.countDown()
            assertTrue(pendingReport.await() is IllegalStateException)
            assertTrue(pendingAsset.await() is IllegalStateException)
            assertTrue(runCatching { report() }.exceptionOrNull() is ApiFailure)
            assertTrue(runCatching { asset() }.exceptionOrNull() is ApiFailure)
            assertEquals(0, other.requestCount)
            settings.saveSession(a, "synthetic-a-again")
            assertEquals(batchId, report().id)
            assertEquals(assetId, asset().id)
            assertEquals(4, origin.requestCount)
            assertEquals(0, other.requestCount)
        } finally {
            release.countDown()
            origin.shutdown()
            other.shutdown()
        }
    }

    private fun syntheticAsset(id: String) = """{"id":"$id","libraryId":"library","fileName":"$id.png","originalFileName":"$id.png",
        "relativePath":"$id.png","mediaType":"image","mimeType":"image/png","size":100,"hash":"fixture","revision":1,
        "tagsRevision":1,"contentVersion":1,"authorId":"","authorIds":[],"authorsRevision":1,"authorStatus":"unknown",
        "tags":[],"favorite":false,"liked":false,"likeCount":0,"note":"","createdAt":"2025-01-01T00:00:00Z",
        "updatedAt":"2025-01-01T00:00:00Z","contentUrl":"media/$id","probeState":"ready","trashed":false,
        "positionMs":0,"positionContentVersion":1}"""

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
