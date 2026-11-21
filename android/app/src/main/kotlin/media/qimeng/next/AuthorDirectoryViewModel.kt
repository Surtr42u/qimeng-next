package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
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
    private val mutableSourceRecord = MutableStateFlow(LoadState<Author>())
    val sourceRecord = mutableSourceRecord.asStateFlow()
    private var selection: Pair<Session, AuthorDirectoryQuery>? = null
    private var read: Job? = null
    private var write: Job? = null
    private var sourceRead: Job? = null

    fun select(target: Session, query: AuthorDirectoryQuery) {
        val next = target to query
        val previous = selection
        if (next == previous) return
        if (previous?.first != target) {
            // 换服务器时清空目录行：保留旧行只适用于同一会话内的搜索/排序/体系重查。
            write?.cancel(); submitting.value = false; total.value = null; mutationError.value = null
            mutable.value = ListState()
            if (previous != null) closeSources()
        }
        selection = next
        load(debounce = previous?.second?.word != query.word && query.word.isNotBlank())
    }

    fun load(next: Boolean = false, debounce: Boolean = false) {
        val current = selection ?: return
        if (next && (mutable.value.loading || mutable.value.cursor == null)) return
        read?.cancel()
        // 第一页重查（搜索/排序/体系）保留当前行，请求成功后再整表替换，列表不再先清空。
        // 第一页重查作废旧游标：失败后的「重试」仍从第一页开始，不会把旧游标接到新查询上。
        val previous: ListState<Author> = mutable.value.copy(loading = false)
        val inFlight = previous.copy(loading = true, error = null, cursor = if (next) previous.cursor else null)
        mutable.value = inFlight
        read = viewModelScope.launch {
            try {
                if (debounce) delay(250)
                val (target, query) = current
                val page = media.authors(cursor = if (next) previous.cursor else null, query = query.word.takeIf(String::isNotBlank), kind = query.kind, sort = query.sort, target = target)
                val count = when {
                    query.word.isBlank() && query.kind == null -> page.totalMatched
                    next -> total.value
                    else -> media.authors(limit = 1, target = target).totalMatched
                }
                if (selection == current && session.value == target) {
                    total.value = count
                    mutable.value = ListState(if (next) (previous.items + page.items).distinctBy(Author::id) else page.items,
                        page.nextCursor, loaded = true, totalMatched = page.totalMatched)
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutable.value = inFlight.copy(loading = false, error = libraryError(failure)) }
        }
    }
    fun dismissError() { mutationError.value = null }
    fun loadSources(authorId: String) {
        val target = session.value ?: return
        sourceRead?.cancel()
        mutableSourceRecord.value = LoadState(loading = true)
        sourceRead = viewModelScope.launch {
            try {
                val author = media.author(authorId, target)
                if (isActive && session.value == target) mutableSourceRecord.value = LoadState(data = author)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (isActive && session.value == target) mutableSourceRecord.value = LoadState(error = libraryError(failure))
            }
        }
    }
    fun closeSources() {
        sourceRead?.cancel()
        mutableSourceRecord.value = LoadState()
    }
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
