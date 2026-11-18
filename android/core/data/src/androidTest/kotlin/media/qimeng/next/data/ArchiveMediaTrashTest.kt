package media.qimeng.next.data

import android.content.ContentValues
import android.graphics.Bitmap
import android.provider.MediaStore
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.*
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.security.MessageDigest
import java.util.UUID

class ArchiveMediaTrashTest {
    @Test fun sameBytesAfterRenameNeedsFreshBindingAndSuccessfulTrashHasReplayableReceipt() = runBlocking {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val resolver = context.contentResolver
        val name = UUID.randomUUID().toString()
        val metadata = ContentValues().apply {
            put(MediaStore.MediaColumns.DISPLAY_NAME, "$name.png")
            put(MediaStore.MediaColumns.MIME_TYPE, "image/png")
            put(MediaStore.MediaColumns.RELATIVE_PATH, "Pictures/QimengArchiveFixture")
            put(MediaStore.MediaColumns.IS_PENDING, 1)
        }
        val media = requireNotNull(resolver.insert(MediaStore.Images.Media.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY), metadata))
        try {
            val bitmap = Bitmap.createBitmap(32, 20, Bitmap.Config.ARGB_8888).apply { eraseColor(0xff3aaf91.toInt()) }
            val bytes = ByteArrayOutputStream().use { out -> try { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, out)); out.toByteArray() } finally { bitmap.recycle() } }
            requireNotNull(resolver.openOutputStream(media, "w")).use { it.write(bytes) }
            metadata.clear(); metadata.put(MediaStore.MediaColumns.IS_PENDING, 0)
            assertEquals(1, resolver.update(media, metadata, null, null))
            val port = ArchiveMediaTrash(context)
            val original = requireNotNull(port.identifyMedia(media)) { "The owned system MediaStore row must have a physical binding" }
            val hash = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
            metadata.clear(); metadata.put(MediaStore.MediaColumns.DISPLAY_NAME, "$name-renamed.png")
            assertEquals(1, resolver.update(media, metadata, null, null))
            assertFalse(port.matches(original, bytes.size.toLong(), hash))
            // MediaProvider may still advance the row generation while scanning the renamed file.
            val binding = withTimeout(5_000) {
                var previous = port.identifyMedia(media)
                while (true) {
                    delay(100)
                    val current = port.identifyMedia(media)
                    if (current != null && current == previous) break
                    previous = current
                }
                requireNotNull(previous)
            }
            assertTrue(port.matches(binding, bytes.size.toLong(), hash))
            assertTrue(port.move(binding, bytes.size.toLong()))
            val observed = port.observe(binding, bytes.size.toLong(), hash, "synthetic-permit")
            assertEquals("recycled", observed.outcome)
            assertEquals(64, observed.receipt?.length)
            assertEquals(observed, port.observe(binding, bytes.size.toLong(), hash, "synthetic-permit"))
        } finally {
            // This exact item URI was allocated by the test. No directory or collection deletion.
            resolver.delete(media, null, null)
        }
    }
}
