package media.qimeng.next.test

import android.graphics.Bitmap
import android.graphics.Color
import android.net.Uri
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.delay
import kotlinx.coroutines.withTimeout
import media.qimeng.next.data.DataGraph
import media.qimeng.next.model.*
import okhttp3.Request
import org.json.JSONObject
import org.junit.Assert.*
import java.util.UUID

/** Both the provider's declared zero and an unknown size followed by EOF reach the actual Go manifest. */
internal suspend fun mixedUploadRoundTrip(graph: DataGraph) {
    val target = requireNotNull(graph.settings.session.value)
    require(target.baseUrl == "http://127.0.0.1:18481/")
    val resolver = InstrumentationRegistry.getInstrumentation().targetContext.contentResolver
    val names = listOf("known-empty.png", "mixed-notes.txt", "unknown-empty.png", "mixed-good.png")
    val uris = names.map { Uri.parse("content://media.qimeng.next.test.fixture/$it") }
    for (mode in listOf(UploadMode.ORDINARY, UploadMode.INTELLIGENT)) {
        val key = UUID.randomUUID().toString()
        val library = graph.media.createLibrary("混合输入 $key", false)
        requireNotNull(resolver.openOutputStream(uris[0], "wt")).close()
        requireNotNull(resolver.openOutputStream(uris[1], "wt")).use { it.write("synthetic text".toByteArray()) }
        requireNotNull(resolver.openOutputStream(uris[2], "wt")).close()
        val image = Bitmap.createBitmap(20, 20, Bitmap.Config.ARGB_8888)
        image.eraseColor(Color.CYAN)
        image.setPixel(0, 0, Color.rgb(key.hashCode() and 255, key.hashCode().ushr(8) and 255, key.hashCode().ushr(16) and 255))
        requireNotNull(resolver.openOutputStream(uris[3], "wt")).use { assertTrue(image.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        image.recycle()

        val ids = graph.uploads.receive(uris, intakeGroup = key)
        val originals = graph.queue.state.value!!.pendingInputs.filter { it.intakeGroup == key }
        assertEquals(ids, originals.map { it.id })
        assertEquals(names, originals.map { it.originalFileName })
        assertEquals(listOf(0L, 0L), listOf(originals[0].size, originals[2].size))
        val family = if (mode == UploadMode.ORDINARY) "合成混合命名" else null
        var expectedPath: String? = null
        if (family != null) {
            val request = NamingRequest(library.id, "", family, null, "image", "",
                originals.filter { it.size != 0L }.map { NamingFile(it.id, requireNotNull(it.originalFileName)) })
            assertTrue(graph.naming.inspect(request).unsupportedInputs)
            expectedPath = requireNotNull(graph.naming.inspect(request.copy(files = listOf(request.files.last()))).preview).names.single()
        }
        val grant = if (mode == UploadMode.INTELLIGENT) graph.aiAccess.create(AIAccessPolicy(
            label = "Mixed intake", libraryIds = listOf(library.id), authorIds = emptyList(), allAuthors = false,
            allowedFields = emptyList(), candidateBlocks = emptyList(), allowClear = false, canIngest = true, canResearch = false,
            canOrganize = true, maxResearchCalls = 0, maxUpdates = 1, maxTasksPerDay = 1, validDays = 1, maxCallsPerTask = 50, taskSeconds = 300)).first else null
        try {
            graph.uploads.enqueue(library.id, "", mode, UploadOptions(family = family), target, ids, batchKey = key)
            withTimeout(90_000) {
                while (true) {
                    val rows = graph.queue.state.value!!.transfers.filter { it.batchKey == key }
                    check(rows.none { it.state == TransferState.FAILED }) { rows.joinToString { it.message } }
                    if (rows.size == ids.size && rows.all { it.state.isFinished() }) break
                    delay(200)
                }
            }
            val journal = graph.queue.state.value!!
            val rows = journal.transfers.filter { it.batchKey == key }
            assertEquals(ids, rows.map { it.id })
            assertEquals(List(3) { TransferState.REJECTED } + TransferState.SUCCEEDED, rows.map { it.state })
            assertTrue(rows.take(3).all { it.uploadId == null && it.assetId == null })
            val batchId = requireNotNull(journal.batches.single { it.key == key }.serverId)
            val report = graph.media.report(batchId)
            assertEquals(ids, report.items.map { it.id })
            assertEquals(listOf("failed", "failed", "failed", if (mode == UploadMode.ORDINARY) "ingested" else "staged"), report.items.map { it.state })
            assertEquals(rows.take(3).map { it.message }, report.items.take(3).map { it.message })
            val request = Request.Builder().url(target.baseUrl + "api/v1/batches/" + batchId).build()
            val receipt = graph.media.mediaClient(target).newCall(request).execute().use { response ->
                assertTrue(response.isSuccessful)
                JSONObject(requireNotNull(response.body).string())
            }
            val inputs = receipt.getJSONArray("inputs")
            assertEquals(listOf("empty_input", "unsupported_media", "empty_input"), (0..2).map { inputs.getJSONObject(it).getString("errorCode") })
            assertTrue((0..2).all { inputs.getJSONObject(it).isNull("uploadId") && inputs.getJSONObject(it).isNull("assetId") })
            assertEquals(1, receipt.getJSONArray("items").length())
            assertEquals(rows.last().uploadId, inputs.getJSONObject(3).getString("uploadId"))
            if (mode == UploadMode.ORDINARY) {
                val asset = graph.media.assets(AssetQuery(libraryId = library.id)).items.single()
                assertEquals(expectedPath, asset.relativePath)
                assertEquals(rows.last().assetId, asset.id)
                assertEquals(0, receipt.getJSONObject("batch").getInt("researchCalls"))
                assertTrue(receipt.isNull("executorStatus"))
            } else assertEquals("awaiting_claim", receipt.getJSONObject("executorStatus").getString("state"))
            // A previous client saved only the seal and left these inputs in its retryable failure state.
            graph.queue.update { state -> state.copy(
                batches = state.batches.map { if (it.key == key) it.copy(inputsChecked = false) else it },
                transfers = state.transfers.map { if (it.batchKey == key && it.state == TransferState.REJECTED)
                    it.copy(state = TransferState.FAILED, message = "旧队列失败") else it }) }
            graph.queue.load()
            graph.uploads.schedule()
            withTimeout(30_000) {
                while (!graph.queue.state.value!!.batches.single { it.key == key }.inputsChecked) delay(100)
            }
            graph.uploads.retry(ids.first())
            assertEquals(rows, graph.queue.state.value!!.transfers.filter { it.batchKey == key })
            graph.uploads.clearFinished()
            assertTrue(graph.queue.state.value!!.transfers.none { it.batchKey == key })
            assertEquals(report.items, graph.media.report(batchId).items)
        } finally {
            if (grant != null) graph.aiAccess.revoke(grant.id)
        }
    }
}
