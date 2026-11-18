package media.qimeng.next.data

import android.graphics.Bitmap
import android.graphics.Color
import kotlinx.coroutines.runBlocking
import media.qimeng.next.model.Asset
import media.qimeng.next.sdk.model.EventCreate
import media.qimeng.next.sdk.model.TermInput
import media.qimeng.next.sdk.model.UploadCreate
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import org.junit.Assert.*
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.util.UUID

class DeviceReactionIntentTest {
    @Test fun independentSessionsKeepFavoritesAndHistoryWhenTodaysVoteIsRetriedOrCancelled() = runBlocking {
        val first = signInToCloud()
        val second = signInToCloud()
        assertNotEquals(first.api.getCurrentSession().bodyOrThrow().id, second.api.getCurrentSession().bodyOrThrow().id)
        val asset = createPicture(first)
        try {
            val before = first.api.getAsset(asset.id).bodyOrThrow()
            val event = EventCreate(UUID.randomUUID().toString(), EventCreate.Kind.view, asset.version, sessionId = UUID.randomUUID().toString())
            first.api.recordEvent(asset.id, event).bodyOrThrow()
            val viewId = second.media.history().items.single { it.assetId == asset.id }.id
            val stale = second.media.asset(asset.id)
            assertFalse(stale.favorite)
            assertFalse(stale.liked)
            assertEquals(before.likeCount, stale.likeCount)
            val firstSnapshot = first.media.setFavorite(asset, true, "android-favorite-other-session")
            assertTrue(firstSnapshot.favorite)
            assertFalse(firstSnapshot.liked)

            val likeKey = "android-today-like-once"
            val liked = second.media.setTodayLike(stale, true, likeKey)
            assertTrue(liked.favorite && liked.liked)
            assertEquals(before.likeCount + 1, liked.likeCount)
            second.media.setTodayLike(stale, true, likeKey)
            assertEquals(before.likeCount + 1, first.media.asset(asset.id).likeCount)

            val unfavorited = first.media.setFavorite(firstSnapshot, false, "android-remove-favorite")
            assertTrue(unfavorited.liked)
            assertFalse(unfavorited.favorite)
            assertEquals(before.likeCount + 1, unfavorited.likeCount)
            first.media.setFavorite(firstSnapshot, true, "android-restore-favorite")

            val cancelled = second.media.setTodayLike(liked, false, "android-cancel-today-like")
            assertTrue(cancelled.favorite)
            assertFalse(cancelled.liked)
            // A late retransmission of the first click must not restore a vote that the user cancelled.
            second.media.setTodayLike(stale, true, likeKey)
            val final = first.api.getAsset(asset.id).bodyOrThrow()
            assertTrue(final.favorite)
            assertFalse(final.liked)
            assertEquals(before.likeCount, final.likeCount)
            assertEquals(before.unattributedLikeCount, final.unattributedLikeCount)
            assertEquals(viewId, first.media.history().items.single { it.assetId == asset.id }.id)
        } finally {
            first.media.trash(first.media.asset(asset.id))
            first.media.logout()
            second.media.logout()
        }
    }

    @Test fun removingOnlyACharacterDoesNotConfirmItsDisplayedSource() = runBlocking {
        val client = signInToCloud()
        val asset = createPicture(client, "android-taxonomy", Color.GREEN)
        try {
            val source = client.api.createSource("android-taxonomy-source", TermInput(name = "云端关系出处", aliases = emptyList())).bodyOrThrow()
            val role = client.api.createCharacter("android-taxonomy-role", TermInput(name = "云端关系角色", aliases = emptyList(), sourceId = source.id)).bodyOrThrow()
            val linked = client.media.updateRelations(asset, emptyList(), listOf(role.id))
            assertEquals(listOf(source.id), linked.sourceIds)
            val cleared = client.media.updateRelations(linked, linked.sourceIds, emptyList())
            assertTrue(cleared.sourceIds.isEmpty() && cleared.characterIds.isEmpty())
            assertEquals(cleared.sourceIds, client.media.asset(asset.id).sourceIds)
        } finally {
            client.media.trash(client.media.asset(asset.id))
            client.media.logout()
        }
    }

    private suspend fun createPicture(client: CloudClient, key: String = "android-reaction", color: Int = Color.BLUE): Asset {
        val library = client.media.createLibrary("云端样本 $key", false)
        val bitmap = Bitmap.createBitmap(24, 32, Bitmap.Config.ARGB_8888)
        val bytes = try {
            bitmap.eraseColor(color)
            ByteArrayOutputStream().use { stream -> check(bitmap.compress(Bitmap.CompressFormat.PNG, 100, stream)); stream.toByteArray() }
        } finally { bitmap.recycle() }
        val input = UploadCreate(libraryId = library.id, fileName = "reaction-test.png", propertySize = bytes.size.toLong(), mimeType = "image/png", mode = UploadCreate.Mode.ordinary)
        val upload = client.api.createUpload("$key-upload", input).bodyOrThrow()
        client.api.appendUpload(upload.id, 0L, bytes.toRequestBody("application/octet-stream".toMediaType())).bodyOrThrow()
        val result = client.api.completeUpload(upload.id, "$key-complete").bodyOrThrow()
        return client.media.asset(requireNotNull(result.upload.assetId))
    }

}
