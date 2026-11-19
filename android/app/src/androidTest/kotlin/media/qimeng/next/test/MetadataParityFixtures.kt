package media.qimeng.next.test

import android.graphics.Bitmap
import android.graphics.Color
import android.net.Uri
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.delay
import kotlinx.coroutines.withTimeout
import media.qimeng.next.data.DataGraph
import media.qimeng.next.model.Asset
import media.qimeng.next.model.Session
import media.qimeng.next.model.TransferState
import media.qimeng.next.model.UploadMode
import media.qimeng.next.model.UploadOptions
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import org.junit.Assert.assertTrue
import java.io.ByteArrayOutputStream
import java.util.UUID

internal fun writeMetadataImage(suffix: String): Pair<Uri, ByteArray> {
    val resolver = InstrumentationRegistry.getInstrumentation().targetContext.contentResolver
    val uri = Uri.parse("content://media.qimeng.next.test.fixture/mixed-good.png")
    val image = Bitmap.createBitmap(40, 32, Bitmap.Config.ARGB_8888)
    image.eraseColor(Color.YELLOW)
    val hash = suffix.hashCode()
    image.setPixel(0, 0, Color.rgb(hash and 255, hash.ushr(8) and 255, hash.ushr(16) and 255))
    val bytes = ByteArrayOutputStream().use {
        assertTrue(image.compress(Bitmap.CompressFormat.PNG, 100, it))
        it.toByteArray()
    }
    image.recycle()
    requireNotNull(resolver.openOutputStream(uri, "wt")).use { it.write(bytes) }
    return uri to bytes
}

internal suspend fun seedMetadataAsset(graph: DataGraph, target: Session, library: String, name: String, authorIds: List<String> = emptyList()): Asset {
    val key = UUID.randomUUID().toString()
    val (uri, _) = writeMetadataImage(name)
    val id = graph.uploads.receive(listOf(uri), intakeGroup = key).single()
    graph.uploads.renameDraft(id, name)
    graph.uploads.enqueue(library, "", UploadMode.ORDINARY, UploadOptions(authorIds = authorIds), target, listOf(id), batchKey = key)
    return withTimeout(60_000) {
        var transfer = graph.queue.state.value!!.transfers.single { it.id == id }
        while (transfer.state != TransferState.SUCCEEDED) {
            check(transfer.state !in setOf(TransferState.FAILED, TransferState.REJECTED, TransferState.CANCELLED)) { transfer.message }
            delay(200)
            transfer = graph.queue.state.value!!.transfers.single { it.id == id }
        }
        graph.media.asset(requireNotNull(transfer.assetId), target)
    }
}

internal fun setSyntheticAcquisitionOptions(graph: DataGraph, target: Session, first: String, second: String) {
    require(target.baseUrl == "http://127.0.0.1:18481/")
    val url = target.baseUrl + "api/v1/acquisition-vocabulary"
    val client = graph.media.mediaClient(target)
    val current = client.newCall(Request.Builder().url(url).build()).execute().use { response ->
        assertTrue(response.isSuccessful)
        JSONObject(requireNotNull(response.body).string())
    }
    val body = JSONObject().put("expectedRevision", current.getLong("revision"))
        .put("sources", current.getJSONArray("sources").put("$first $second"))
    val update = Request.Builder().url(url).header("Idempotency-Key", UUID.randomUUID().toString())
        .put(body.toString().toRequestBody("application/json".toMediaType())).build()
    client.newCall(update).execute().use { assertTrue("Acquisition vocabulary returned HTTP ${it.code}", it.isSuccessful) }
}
