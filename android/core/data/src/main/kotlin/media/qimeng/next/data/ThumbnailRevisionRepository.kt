package media.qimeng.next.data

import android.util.Log
import com.google.gson.Gson
import com.google.gson.annotations.SerializedName
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.suspendCancellableCoroutine
import media.qimeng.next.model.Session
import okhttp3.Call
import okhttp3.Callback
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Request
import okhttp3.Response
import okio.Buffer
import java.io.IOException
import java.util.concurrent.TimeUnit

private const val MAX_REVISION_BYTES = 64L * 1024

internal data class ThumbnailRevisionSnapshot(
    val instanceId: String,
    val revision: Long,
    val thumbnailVersion: String,
)

/**
 * Raw client for `GET /api/v1/library/revision` (OpenAPI contract added with the thumbnail
 * migration). A raw call is used because the Android SDK is generated separately and may not
 * contain the new operation yet.
 *
 * Any failure - old server 404, malformed payload, another instance answering, network error -
 * returns null, which the prefetch round treats as "run a full round". This keeps the default
 * weighted towards fetching too much rather than incorrectly skipping.
 */
internal class ThumbnailRevisionRepository(private val provider: ApiProvider) {
    private val gson = Gson()

    suspend fun fetch(target: Session): ThumbnailRevisionSnapshot? = try {
        val url = target.baseUrl.toHttpUrl().newBuilder()
            .addPathSegments("api/v1/library/revision")
            .build()
        val call = provider.client(target).newCall(
            Request.Builder().url(url).header("Accept", "application/json").build(),
        )
        call.timeout().timeout(20, TimeUnit.SECONDS)
        suspendCancellableCoroutine<ThumbnailRevisionSnapshot?> { continuation ->
            continuation.invokeOnCancellation { call.cancel() }
            call.enqueue(object : Callback {
                override fun onFailure(call: Call, e: IOException) {
                    continuation.resumeWith(Result.failure(e))
                }

                override fun onResponse(call: Call, response: Response) {
                    continuation.resumeWith(runCatching { response.use { parse(it, target.instanceId) } })
                }
            })
        }
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (failure: Exception) {
        Log.d("ThumbnailCache", "Library revision unavailable; a full prefetch round will run", failure)
        null
    }

    private fun parse(response: Response, expectedInstanceId: String): ThumbnailRevisionSnapshot? {
        if (response.code != 200) return null
        val body = response.body ?: return null
        if (body.contentLength() > MAX_REVISION_BYTES) return null
        val buffer = Buffer()
        val source = body.source()
        while (true) {
            val read = source.read(buffer, minOf(4096L, MAX_REVISION_BYTES + 1 - buffer.size))
            if (read == -1L) break
            if (buffer.size > MAX_REVISION_BYTES) return null
        }
        val payload = gson.fromJson(buffer.readUtf8(), RevisionPayload::class.java) ?: return null
        // A proxy or a different instance behind the same origin must not authorize a skip.
        val instanceId = payload.instanceId?.takeIf { it == expectedInstanceId } ?: return null
        val revision = payload.revision?.takeIf { it >= 0 } ?: return null
        val thumbnailVersion = payload.thumbnailVersion?.takeIf { it.isNotBlank() } ?: return null
        return ThumbnailRevisionSnapshot(instanceId, revision, thumbnailVersion)
    }

    private data class RevisionPayload(
        @SerializedName("instanceId") val instanceId: String? = null,
        @SerializedName("revision") val revision: Long? = null,
        @SerializedName("thumbnailVersion") val thumbnailVersion: String? = null,
    )
}
