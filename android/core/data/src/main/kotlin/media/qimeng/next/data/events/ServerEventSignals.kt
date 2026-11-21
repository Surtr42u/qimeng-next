package media.qimeng.next.data.events

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/**
 * The single mapping from a stream event to the data a screen must reload.
 *
 * Each counter is monotonic. A screen remembers the value it last rendered and
 * reloads when the current value differs; it never needs the event payload or
 * a replay of missed frames. Reconnecting bumps every counter once, because
 * events missed while disconnected cannot be recovered: the safe compensation
 * is one extra authoritative read (idempotent by construction).
 */
class ServerEventSignals {

    private val mutableLibrary = MutableStateFlow(0L)
    private val mutableFavorite = MutableStateFlow(0L)
    private val mutableLike = MutableStateFlow(0L)

    /** Library content: asset lists, details, authors, collections, trash, scans. */
    val library: StateFlow<Long> = mutableLibrary.asStateFlow()

    /** Favorite state shown on any screen. */
    val favorite: StateFlow<Long> = mutableFavorite.asStateFlow()

    /** Dated vote state shown on any screen. */
    val like: StateFlow<Long> = mutableLike.asStateFlow()

    internal fun received(type: String?) {
        when (type) {
            ServerEventTopics.LIBRARY_CHANGED, ServerEventTopics.UPLOAD_DONE -> bump(mutableLibrary)
            ServerEventTopics.FAVORITE_CHANGED -> bump(mutableFavorite)
            ServerEventTopics.LIKE_CHANGED -> bump(mutableLike)
            // hello and topics this client does not render are ignored.
        }
    }

    internal fun streamOpened() {
        bump(mutableLibrary)
        bump(mutableFavorite)
        bump(mutableLike)
    }

    private fun bump(counter: MutableStateFlow<Long>) {
        counter.update { it + 1 }
    }
}
