package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.model.Library
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget
import media.qimeng.next.runtime.LocalFolderPage
import java.util.UUID

class LibraryManagementViewModel(application: Application) : AndroidViewModel(application) {
    private val app = application as QimengApplication
    private val graph = app.graph
    private val media = graph.media
    val session = media.session
    val fileAccess = app.runtime.files.access
    val localScans = app.runtime.scans.state
    private val mutableList = MutableStateFlow(ListState<Library>())
    val libraries = mutableList.asStateFlow()
    private val mutableBusy = MutableStateFlow(false)
    val busy = mutableBusy.asStateFlow()
    private val mutableNotice = MutableStateFlow<String?>(null)
    val notice = mutableNotice.asStateFlow()
    private var listJob: Job? = null
    private var operation: Job? = null
    private val retryScanKeys = mutableMapOf<Pair<String, Long>, String>()

    init {
        viewModelScope.launch {
            session.collect {
                listJob?.cancel(); operation?.cancel()
                mutableList.value = ListState(); mutableBusy.value = false; mutableNotice.value = null
                retryScanKeys.clear()
                if (it != null) refresh()
            }
        }
    }

    fun refresh(next: Boolean = false) {
        val target = session.value ?: return
        val previous = if (next) mutableList.value else ListState()
        if (next && (previous.loading || previous.cursor == null)) return
        listJob?.cancel()
        mutableList.value = mutableList.value.copy(loading = true, error = null)
        listJob = viewModelScope.launch {
            try {
                val page = media.libraryPage(previous.cursor)
                if (sameTarget(target, session.value)) mutableList.value = ListState((previous.items + page.items).distinctBy(Library::id),
                    page.nextCursor, loaded = true, totalMatched = page.totalMatched)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutableList.value = mutableList.value.copy(loading = false, error = libraryError(failure)) }
        }
    }

    fun owned(target: Session) = app.runtime.available && target.baseUrl in app.runtime.ownedEndpoints(target.instanceId)
    fun requestFileAccess() = app.runtime.files.requestAccess()
    fun refreshAccess() = app.runtime.files.refreshAccess()
    suspend fun folders(path: String?, cursor: String?): LocalFolderPage = app.runtime.files.folders(path, cursor)
    suspend fun location(target: Session, id: String) = media.libraryRoot(target, id)
    fun clearNotice() { mutableNotice.value = null }

    fun register(name: String, path: String, cos: Boolean, key: String, registered: () -> Unit) = mutate { target ->
        val library = media.registerLibrary(target, name, path, cos, key)
        registered()
        mutableList.value = mutableList.value.copy(items = (mutableList.value.items.filterNot { it.id == library.id } + library))
        try {
            startScan(target, library, "$key-scan")
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) { mutableNotice.value = "已注册，扫描尚未确认。${libraryError(failure)}，可在该库重新扫描。" }
    }

    fun rescan(library: Library) = mutate { target -> startScan(target, library) }
    private suspend fun startScan(target: Session, library: Library, registerKey: String? = null) {
        check(sameTarget(target, session.value)) { "连接已改变，请在当前服务器重新核对媒体库" }
        if (owned(target)) {
            app.runtime.scans.request(target.instanceId, library.id, library.revision)
            mutableNotice.value = if (localScans.value.chargeOnly) "已加入仅充电时扫描队列" else "已加入本机扫描队列"
        } else {
            val request = library.id to library.revision
            val key = retryScanKeys.getOrPut(request) { registerKey ?: UUID.randomUUID().toString() }
            graph.scans.start(target, library, key)
            retryScanKeys.remove(request)
            mutableNotice.value = "已提交扫描"
        }
    }

    fun enabled(library: Library, enabled: Boolean) = mutate { media.updateLibrary(library, library.name, enabled) }
    fun delete(library: Library) = mutate { target ->
        media.unregisterLibrary(target, library)
        if (owned(target)) {
            try { app.runtime.scans.forget(target.instanceId, library.id) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                mutableNotice.value = "库索引已删除。本机待扫记录未能清理，${libraryError(failure)}"
                return@mutate
            }
        }
        mutableNotice.value = "已删除「${library.name}」的媒体索引"
    }

    private fun mutate(work: suspend (Session) -> Unit) {
        val target = session.value ?: return
        if (mutableBusy.value) return
        mutableBusy.value = true; mutableNotice.value = null
        operation = viewModelScope.launch {
            try { work(target) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutableNotice.value = libraryError(failure) }
            finally { if (sameTarget(target, session.value)) { mutableBusy.value = false; refresh() } }
        }
    }
}

internal fun libraryError(failure: Exception): String = when (failure) {
    is ApiFailure -> failure.message
    is IllegalArgumentException, is IllegalStateException -> failure.message ?: "操作未完成"
    else -> "读取或提交失败，请重试"
}
