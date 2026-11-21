package media.qimeng.next.data

import android.content.Context
import android.util.Log

internal const val THUMBNAIL_PREFETCH_PREFERENCES = "thumbnail-prefetch"

/** The revision read at the start of the last completed round, paired with its server identity. */
internal data class ThumbnailPrefetchRecord(
    val serverKey: String,
    val revision: Long,
    val thumbnailVersion: String,
)

/**
 * Persists the pair (serverKey, revision, thumbnailVersion) plus up to 64 sampled cache keys.
 *
 * The sample is written in the same SharedPreferences edit as the revision so a skip decision
 * can never observe a record without its sample. [clear] invalidates the record whenever a
 * cache pool is emptied; otherwise a static library would keep skipping an empty cache.
 *
 * [scope] separates the physical cache pools (nas / local). Clearing one pool must not
 * invalidate the other pool's skip evidence: with a shared record, clearing NAS makes the next
 * local round re-list the whole library and vice versa. The empty scope keeps the historical
 * single-record file used before pooling (and by this store's instrumentation test).
 */
internal class ThumbnailPrefetchRevisionStore(context: Context, scope: String = "") {
    private val storage = context.getSharedPreferences(
        if (scope.isEmpty()) THUMBNAIL_PREFETCH_PREFERENCES else "$THUMBNAIL_PREFETCH_PREFERENCES-$scope",
        Context.MODE_PRIVATE,
    )

    fun lastDone(): ThumbnailPrefetchRecord? {
        if (!storage.contains(KEY_SERVER) || !storage.contains(KEY_REVISION)) return null
        val serverKey = storage.getString(KEY_SERVER, null)?.takeIf { it.isNotEmpty() } ?: return null
        val revision = storage.getLong(KEY_REVISION, -1L).takeIf { it >= 0 } ?: return null
        return ThumbnailPrefetchRecord(
            serverKey = serverKey,
            revision = revision,
            thumbnailVersion = storage.getString(KEY_THUMBNAIL_VERSION, "").orEmpty(),
        )
    }

    fun setLastDone(record: ThumbnailPrefetchRecord, sample: Set<ThumbnailPrefetchSample>) {
        val editor = storage.edit()
            .putString(KEY_SERVER, record.serverKey)
            .putLong(KEY_REVISION, record.revision)
            .putString(KEY_THUMBNAIL_VERSION, record.thumbnailVersion)
            .putStringSet(KEY_SAMPLE, sample.map(ThumbnailPrefetchSample::encode).toSet())
        if (!editor.commit()) Log.w("ThumbnailCache", "Prefetch revision record could not be saved")
    }

    fun storedSample(): Set<ThumbnailPrefetchSample> =
        storage.getStringSet(KEY_SAMPLE, emptySet()).orEmpty()
            .mapNotNull { ThumbnailPrefetchSample.decode(it) }
            .toSet()

    fun clear() {
        val editor = storage.edit()
            .remove(KEY_SERVER)
            .remove(KEY_REVISION)
            .remove(KEY_THUMBNAIL_VERSION)
            .remove(KEY_SAMPLE)
        if (!editor.commit()) Log.w("ThumbnailCache", "Prefetch revision record could not be cleared")
    }

    private companion object {
        val KEY_SERVER = "server-key"
        val KEY_REVISION = "revision"
        val KEY_THUMBNAIL_VERSION = "thumbnail-version"
        val KEY_SAMPLE = "sample"
    }
}
