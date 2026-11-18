package media.qimeng.next.data

import android.content.ContextWrapper
import android.content.SharedPreferences
import android.graphics.Bitmap
import android.graphics.Color
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.runBlocking
import media.qimeng.next.model.Asset
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.sdk.model.CommitV2MigrationRequest
import media.qimeng.next.sdk.model.EventCreate
import media.qimeng.next.sdk.model.UploadCreate
import media.qimeng.next.sdk.model.V2PlanCreate
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.util.UUID

class DeviceReactionIntentTest {
    private data class Client(val media: MediaRepository, val api: DefaultApi)

    @Test fun independentSessionsKeepFavoritesAndHistoricalVotesWhenTodaysVoteIsRetriedOrCancelled() = runBlocking {
        val first = signIn()
        val second = signIn()
        assertNotEquals(first.api.getCurrentSession().bodyOrThrow().id, second.api.getCurrentSession().bodyOrThrow().id)
        val asset = createPicture(first)
        try {
            importPastVotes(first.api, asset)
            val event = EventCreate(UUID.randomUUID().toString(), EventCreate.Kind.view, asset.version, sessionId = UUID.randomUUID().toString())
            first.api.recordEvent(asset.id, event).bodyOrThrow()
            val viewId = second.media.history().items.single { it.assetId == asset.id }.id
            val stale = second.media.asset(asset.id)
            assertFalse(stale.favorite)
            assertFalse(stale.liked)
            assertEquals(4L, stale.likeCount)
            val firstSnapshot = first.media.setFavorite(asset, true, "android-favorite-other-session")
            assertTrue(firstSnapshot.favorite)
            assertFalse(firstSnapshot.liked)

            val likeKey = "android-today-like-once"
            val liked = second.media.setTodayLike(stale, true, likeKey)
            assertTrue(liked.favorite && liked.liked)
            assertEquals(5L, liked.likeCount)
            second.media.setTodayLike(stale, true, likeKey)
            assertEquals(5L, first.media.asset(asset.id).likeCount)

            val unfavorited = first.media.setFavorite(firstSnapshot, false, "android-remove-favorite")
            assertTrue(unfavorited.liked)
            assertFalse(unfavorited.favorite)
            assertEquals(5L, unfavorited.likeCount)
            first.media.setFavorite(firstSnapshot, true, "android-restore-favorite")

            val cancelled = second.media.setTodayLike(liked, false, "android-cancel-today-like")
            assertTrue(cancelled.favorite)
            assertFalse(cancelled.liked)
            // A late retransmission of the first click must not restore a vote that the user cancelled.
            second.media.setTodayLike(stale, true, likeKey)
            val final = first.api.getAsset(asset.id).bodyOrThrow()
            assertTrue(final.favorite)
            assertFalse(final.liked)
            assertEquals(4L, final.likeCount)
            assertEquals(3L, final.unattributedLikeCount)
            assertEquals(viewId, first.media.history().items.single { it.assetId == asset.id }.id)
        } finally {
            first.media.trash(first.media.asset(asset.id))
            first.media.logout()
            second.media.logout()
        }
    }

    private suspend fun signIn(): Client {
        val args = InstrumentationRegistry.getArguments()
        val address = requireNotNull(args.getString("qimengServerUrl"))
        require(address == "http://127.0.0.1:18481/")
        val base = InstrumentationRegistry.getInstrumentation().targetContext
        val namespace = UUID.randomUUID().toString()
        val context = object : ContextWrapper(base) {
            override fun getSharedPreferences(name: String, mode: Int): SharedPreferences = base.getSharedPreferences("$namespace-$name", mode)
        }
        val settings = SecureSettings(context)
        val provider = ApiProvider(settings)
        val media = MediaRepository(settings, provider)
        media.login(address, requireNotNull(args.getString("qimengToken")))
        return Client(media, provider.api(requireNotNull(settings.session.value)))
    }

    private suspend fun createPicture(client: Client): Asset {
        val library = client.media.createLibrary("云端独立互动样本", false)
        val bitmap = Bitmap.createBitmap(24, 32, Bitmap.Config.ARGB_8888)
        val bytes = try {
            bitmap.eraseColor(Color.BLUE)
            ByteArrayOutputStream().use { stream -> check(bitmap.compress(Bitmap.CompressFormat.PNG, 100, stream)); stream.toByteArray() }
        } finally { bitmap.recycle() }
        val input = UploadCreate(libraryId = library.id, fileName = "reaction-test.png", propertySize = bytes.size.toLong(), mimeType = "image/png", mode = UploadCreate.Mode.ordinary)
        val upload = client.api.createUpload("android-reaction-upload", input).bodyOrThrow()
        client.api.appendUpload(upload.id, 0L, bytes.toRequestBody("application/octet-stream".toMediaType())).bodyOrThrow()
        val result = client.api.completeUpload(upload.id, "android-reaction-complete").bodyOrThrow()
        return client.media.asset(requireNotNull(result.upload.assetId))
    }

    private suspend fun importPastVotes(api: DefaultApi, asset: Asset) {
        val file = JSONObject().put("recordKey", "picture").put("assetId", asset.id).put("fileName", asset.name).put("mediaType", "image").put("sizeBytes", asset.bytes)
        val votes = JSONObject().put("recordKey", "picture").put("likeCount", 4).put("lastLikeDate", "2020-01-02")
        val data = JSONObject().put("mediaFiles", JSONArray().put(file)).put("likes", JSONArray().put(votes)).put("viewEvents", JSONArray())
        val json = JSONObject().put("format", "qimeng_backup").put("schemaVersion", 2).put("appIdentifier", "synthetic.cloud").put("data", data)
        val body = json.toString().toByteArray(Charsets.UTF_8).toRequestBody("application/octet-stream".toMediaType())
        val staged = api.stageV2Import("android-reaction-stage", "synthetic-reaction-import", body).bodyOrThrow()
        val plan = api.prepareV2Migration(staged.id, "android-reaction-plan", V2PlanCreate()).bodyOrThrow()
        val result = api.commitV2Migration(plan.id, "android-reaction-commit", CommitV2MigrationRequest(plan.fingerprint, false)).bodyOrThrow()
        assertEquals("committed", result.state.value)
    }
}
