package media.qimeng.next

import android.app.Application
import android.net.Uri
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch
import media.qimeng.next.model.ArchiveOverview

class ArchiveViewModel(application: Application) : AndroidViewModel(application) {
    private val app = application as QimengApplication
    private val repo = app.graph.archives
    val tree = repo.tree
    val session = app.graph.settings.session
    private val mutable = MutableStateFlow<ArchiveOverview?>(null)
    val overview = mutable.asStateFlow()
    private val working = MutableStateFlow(false)
    val busy = working.asStateFlow()
    private val failure = MutableStateFlow<String?>(null)
    val error = failure.asStateFlow()

    init {
        viewModelScope.launch {
            combine(session, tree, repo.revision) { target, path, _ -> target to path }.collectLatest { (target, path) ->
                try { mutable.value = if (target == null || path == null) null else repo.latest(target)?.takeIf { it.tree == path } }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (problem: Exception) { failure.value = libraryError(problem) }
            }
        }
    }
    fun select(tree: Uri?, flags: Int = 0) = act { repo.select(tree, flags) }
    fun scan(onlyIfMissing: Boolean = false) = act {
        val target = session.value ?: return@act
        if (tree.value == null) return@act
        val previous = repo.latest(target)
        if (onlyIfMissing && previous != null && previous.tree == tree.value && previous.state != "scanning") { mutable.value = previous; return@act }
        val local = app.runtime.available && target.baseUrl in app.runtime.ownedEndpoints(target.instanceId)
        mutable.value = repo.scan(target, local)
    }
    fun confirm(plan: ArchiveOverview) = act { repo.confirm(plan); mutable.value = repo.latest(plan.target) }
    private fun act(operation: suspend () -> Unit) {
        if (working.value) return
        working.value = true; failure.value = null
        viewModelScope.launch {
            try { operation() }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (problem: Exception) { failure.value = if (problem is SecurityException) "系统未保留目录授权，请重新选择。" else libraryError(problem) }
            finally { working.value = false }
        }
    }
}
