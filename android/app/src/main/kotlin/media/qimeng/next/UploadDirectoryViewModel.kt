package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import media.qimeng.next.model.Directory
import media.qimeng.next.model.Session

/** Only expanded directory branches have a page or a running read. Collapsing releases the subtree. */
class UploadDirectoryViewModel(application: Application) : AndroidViewModel(application) {
    private val media = (application as QimengApplication).graph.media
    private var current: Pair<Session, String>? = null
    private val mutable = MutableStateFlow(emptyMap<String, ListState<Directory>>())
    val branches = mutable.asStateFlow()
    private val jobs = mutableMapOf<String, Job>()

    fun bind(target: Session, library: String, selected: String) {
        if (current == (target to library)) return
        clear()
        current = target to library
        if (library.isEmpty()) return
        open("")
        var parent = ""
        selected.split('/').filter(String::isNotEmpty).forEach { part ->
            parent = if (parent.isEmpty()) part else "$parent/$part"
            open(parent)
        }
    }

    fun clear() {
        jobs.values.forEach(Job::cancel); jobs.clear()
        current = null; mutable.value = emptyMap()
    }

    fun collapse(path: String) {
        val closed = mutable.value.keys.filter { it == path || it.startsWith("$path/") }.toSet()
        closed.forEach { jobs.remove(it)?.cancel() }
        mutable.update { it - closed }
    }

    fun open(path: String, next: Boolean = false) {
        val (target, library) = current ?: return
        val previous = mutable.value[path] ?: ListState()
        if (next && (previous.loading || previous.cursor == null)) return
        jobs[path]?.cancel()
        mutable.update { it + (path to previous.copy(loading = true, error = null)) }
        jobs[path] = viewModelScope.launch {
            try {
                val page = media.directories(target, library, path, if (next) previous.cursor else null)
                if (current == (target to library)) mutable.update { it + (path to ListState(
                    if (next) (previous.items + page.items).distinctBy(Directory::path) else page.items, page.cursor,
                    loaded = true, totalMatched = page.directFileCount)) }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutable.update { it + (path to previous.copy(loading = false, error = libraryError(failure))) } }
        }
    }
}
