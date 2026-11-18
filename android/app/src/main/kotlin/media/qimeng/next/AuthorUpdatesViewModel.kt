package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import media.qimeng.next.model.AuthorFeed
import media.qimeng.next.model.Session
import java.util.UUID

class AuthorUpdatesViewModel(application: Application) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    val session = graph.settings.session
    private val mutable = MutableStateFlow(LoadState<AuthorFeed>())
    val state = mutable.asStateFlow()
    private var scope: Pair<Session, String>? = null
    private var job: Job? = null
    private var checkKey: Pair<Long, String>? = null

    /** Reading cached entries never checks an external feed. Only the user's check button calls checkFeed. */
    fun bind(target: Session, authorId: String, refresh: Boolean = false) {
        val next = target to authorId
        if (scope == next && !refresh) return
        if (scope != next) { mutable.value = LoadState(); checkKey = null }
        scope = next
        run { graph.authorProfiles.feed(target, authorId) }
    }
    fun save(target: Session, base: AuthorFeed, enabled: Boolean, url: String, key: String, done: () -> Unit) {
        if (mutable.value.loading || scope != (target to base.authorId)) return
        run { graph.authorProfiles.saveFeed(target, base, enabled, url, key).also { done() } }
    }
    fun check(target: Session, base: AuthorFeed) {
        if (mutable.value.loading || scope != (target to base.authorId)) return
        val key = checkKey?.takeIf { it.first == base.revision }?.second ?: UUID.randomUUID().toString()
        checkKey = base.revision to key
        run { graph.authorProfiles.checkFeed(target, base, key).also { checkKey = null } }
    }
    private fun run(operation: suspend () -> AuthorFeed) {
        val current = scope ?: return
        job?.cancel()
        mutable.value = mutable.value.copy(loading = true, error = null)
        job = viewModelScope.launch {
            try {
                val value = operation()
                if (scope == current && session.value == current.first) mutable.value = LoadState(value)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (scope == current && session.value == current.first) mutable.value = mutable.value.copy(loading = false, error = libraryError(failure))
            }
        }
    }
}
