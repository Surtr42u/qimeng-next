package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import media.qimeng.next.model.Author
import media.qimeng.next.model.Session

data class AuthorDirectoryQuery(val word: String = "", val kind: String? = null, val sort: String = "default")

/** The directory has independent paging and filters so editing an upload or opening a picker cannot replace its rows. */
class AuthorDirectoryViewModel(application: Application) : AndroidViewModel(application) {
    private val media = (application as QimengApplication).graph.media
    val session = media.session
    private val mutable = MutableStateFlow(ListState<Author>())
    val rows = mutable.asStateFlow()
    private val total = MutableStateFlow<Long?>(null)
    val allAuthors = total.asStateFlow()
    private val submitting = MutableStateFlow(false)
    val busy = submitting.asStateFlow()
    private val mutationError = MutableStateFlow<String?>(null)
    val error = mutationError.asStateFlow()
    private var selection: Pair<Session, AuthorDirectoryQuery>? = null
    private var read: Job? = null
    private var write: Job? = null

    fun select(target: Session, query: AuthorDirectoryQuery) {
        val next = target to query
        val previous = selection
        if (next == previous) return
        if (previous?.first != target) {
            write?.cancel(); submitting.value = false; total.value = null; mutationError.value = null
        }
        selection = next
        load(debounce = previous?.second?.word != query.word && query.word.isNotBlank())
    }

    fun load(next: Boolean = false, debounce: Boolean = false) {
        val current = selection ?: return
        val previous: ListState<Author> = if (next) mutable.value else ListState()
        if (next && (previous.cursor == null || previous.loading)) return
        read?.cancel()
        mutable.value = previous.copy(loading = true, error = null)
        read = viewModelScope.launch {
            try {
                if (debounce) delay(250)
                val (target, query) = current
                val page = media.authors(cursor = previous.cursor, query = query.word.takeIf(String::isNotBlank), kind = query.kind, sort = query.sort, target = target)
                val count = when {
                    query.word.isBlank() && query.kind == null -> page.totalMatched
                    next -> total.value
                    else -> media.authors(limit = 1, target = target).totalMatched
                }
                if (selection == current && session.value == target) {
                    total.value = count
                    mutable.value = ListState((previous.items + page.items).distinctBy(Author::id), page.nextCursor, loaded = true, totalMatched = page.totalMatched)
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutable.value = previous.copy(error = libraryError(failure)) }
        }
    }
    fun dismissError() { mutationError.value = null }
    fun follow(author: Author) = edit { media.follow(author, it) }
    fun create(names: List<String>, key: String, done: () -> Unit) = edit { target ->
        media.createNamedAuthor(target, names, key); total.value = null; done()
    }
    fun rename(author: Author, names: List<String>, done: () -> Unit) = edit { target -> media.renameAuthor(target, author, names); done() }
    fun sources(author: Author, sources: List<String>, done: () -> Unit) = edit { target -> media.saveAuthorSources(target, author, sources); done() }
    fun delete(author: Author, done: () -> Unit) = edit { target -> media.deleteAuthor(author, target); total.value = null; done() }
    private fun edit(operation: suspend (Session) -> Unit) {
        val target = selection?.first ?: return
        if (submitting.value || session.value != target) return
        submitting.value = true; mutationError.value = null
        write = viewModelScope.launch {
            try { operation(target); load() }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutationError.value = libraryError(failure) }
            finally { if (session.value == target) submitting.value = false }
        }
    }
}
