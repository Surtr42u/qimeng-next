package media.qimeng.next.data

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

class ThumbnailPrefetchStoreTest {
    @Test fun recordAndSampleRoundTripAndClear() {
        val context = isolatedContext()
        val store = ThumbnailPrefetchRevisionStore(context)
        assertNull(store.lastDone())
        assertTrue(store.storedSample().isEmpty())

        val record = ThumbnailPrefetchRecord("nas:fixture", 12, ThumbnailCachePolicy.VERSION)
        val sample = setOf(
            ThumbnailPrefetchSample("aa:bb", 3),
            ThumbnailPrefetchSample("cc:dd", 4),
        )
        store.setLastDone(record, sample)
        assertEquals(record, ThumbnailPrefetchRevisionStore(context).lastDone())
        assertEquals(sample, ThumbnailPrefetchRevisionStore(context).storedSample())

        store.clear()
        assertNull(store.lastDone())
        assertTrue(store.storedSample().isEmpty())
    }

    @Test fun aLegacyRevisionWithoutServerKeyIsTreatedAsMissing() {
        val context = isolatedContext()
        context.getSharedPreferences(THUMBNAIL_PREFETCH_PREFERENCES, Context.MODE_PRIVATE).edit()
            .putLong("revision", 5)
            .commit()
        assertNull(ThumbnailPrefetchRevisionStore(context).lastDone())
    }

    private companion object {
        fun isolatedContext(): Context {
            val base = InstrumentationRegistry.getInstrumentation().targetContext
            val suffix = UUID.randomUUID().toString()
            return object : ContextWrapper(base) {
                override fun getSharedPreferences(name: String, mode: Int): SharedPreferences =
                    base.getSharedPreferences("$suffix-$name", mode)
            }
        }
    }
}
