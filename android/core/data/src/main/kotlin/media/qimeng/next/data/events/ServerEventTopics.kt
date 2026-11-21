package media.qimeng.next.data.events

/**
 * Server-sent event topic names, the Android-side single definition point.
 *
 * The server defines the same strings in `server/internal/events/bus.go`.
 * Renaming a topic there requires renaming it here and in [ServerEventSignals];
 * a client that does not know a topic ignores it.
 */
internal object ServerEventTopics {

    /** Stream endpoint relative to the selected server root. */
    const val PATH = "/api/v1/events"

    /** Library content changed: assets, authors, vocabulary, collections, trash. */
    const val LIBRARY_CHANGED = "library.changed"

    /** One asset's favorite state changed. The payload names the asset. */
    const val FAVORITE_CHANGED = "favorite.changed"

    /** One asset's dated vote changed. The payload names the asset. */
    const val LIKE_CHANGED = "like.changed"

    /** One upload became an asset. Library content changed as well. */
    const val UPLOAD_DONE = "upload.done"
}
