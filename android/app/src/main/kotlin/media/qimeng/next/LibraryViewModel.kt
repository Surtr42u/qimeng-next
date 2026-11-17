package media.qimeng.next

import android.app.Application
import android.net.Uri
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.model.*

data class ListState<T>(val items: List<T> = emptyList(), val cursor: String? = null,
    val loading: Boolean = false, val loaded: Boolean = false, val error: String? = null)

class LibraryViewModel(application: Application, private val saved: SavedStateHandle) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    private val repository = graph.media
    val session = repository.session
    val preferences = graph.settings.preferences
    val settingsError = graph.settings.error
    val queue = graph.queue.state
    val queueError = graph.queue.error
    val route = saved.getStateFlow("route", "main")
    val tab = saved.getStateFlow("tab", 0)
    val uploadLibrary = saved.getStateFlow("uploadLibrary", "")
    val uploadDirectory = saved.getStateFlow("uploadDirectory", "")
    val uploadMode = saved.getStateFlow("uploadMode", UploadMode.ORDINARY.name)
    private val mutableMessage = MutableStateFlow<String?>(null)
    val message = mutableMessage.asStateFlow()
    private val mutableBusy = MutableStateFlow(false)
    val busy = mutableBusy.asStateFlow()
    private val mutableLibraries = MutableStateFlow<List<Library>>(emptyList())
    val libraries = mutableLibraries.asStateFlow()
    private val mutableCapabilities = MutableStateFlow<ServerCapabilities?>(null)
    val capabilities = mutableCapabilities.asStateFlow()
    private val mutableAsset = MutableStateFlow<Asset?>(null)
    val asset = mutableAsset.asStateFlow()
    private val mutableAuthor = MutableStateFlow<Author?>(null)
    val author = mutableAuthor.asStateFlow()
    private val mutableReport = MutableStateFlow<TaskReport?>(null)
    val report = mutableReport.asStateFlow()
    private val mutableStats = MutableStateFlow<InventoryStats?>(null)
    val statistics = mutableStats.asStateFlow()
    private val mutableTags = MutableStateFlow<List<String>>(emptyList())
    val tagVocabulary = mutableTags.asStateFlow()
    private val lists = mutableMapOf<String, MutableStateFlow<ListState<Asset>>>()
    private val queries = mutableMapOf<String, AssetQuery>()
    private val listJobs = mutableMapOf<String, Job>()
    private val listVersions = mutableMapOf<String, RequestGeneration>()
    private val authorState = MutableStateFlow(ListState<Author>())
    val authors = authorState.asStateFlow()
    private val historyState = MutableStateFlow(ListState<HistoryEntry>())
    val history = historyState.asStateFlow()
    private val reportState = MutableStateFlow(ListState<TaskReport>())
    val reports = reportState.asStateFlow()
    private val trashState = MutableStateFlow(ListState<Asset>())
    val trash = trashState.asStateFlow()
    private var detailJob: Job? = null
    private var detailGeneration = RequestGeneration()
    private var connectionGeneration = 0L
    private val activeOperations = mutableSetOf<Job>()

    init {
        viewModelScope.launch {
            session.collect { connection ->
                connectionGeneration++
                activeOperations.toList().forEach(Job::cancel)
                mutableBusy.value = false
                listJobs.values.forEach(Job::cancel)
                detailJob?.cancel()
                detailGeneration.next()
                lists.values.forEach { it.value = ListState() }
                mutableAsset.value = null; mutableAuthor.value = null; mutableStats.value = null
                mutableLibraries.value = emptyList(); mutableCapabilities.value = null
                authorState.value = ListState(); historyState.value = ListState(); reportState.value = ListState()
                trashState.value = ListState()
                if (connection != null) {
                    saved["uploadLibrary"] = graph.settings.uploadLibrary(connection).orEmpty()
                    refreshConnection()
                }
            }
        }
    }

    fun assets(key: String): StateFlow<ListState<Asset>> = lists.getOrPut(key) { MutableStateFlow(ListState()) }.asStateFlow()
    fun query(key: String): AssetQuery = queries[key] ?: AssetQuery()
    fun browse(key: String, query: AssetQuery = query(key), next: Boolean = false, force: Boolean = false) {
        val state = lists.getOrPut(key) { MutableStateFlow(ListState()) }
        val previous = queries[key]
        if (next && (state.value.loading || state.value.cursor == null)) return
        if (!next && !force && previous == query && (state.value.loaded || state.value.loading)) return
        queries[key] = query
        listJobs[key]?.cancel()
        val generation = listVersions.getOrPut(key) { RequestGeneration() }
        val ticket = generation.next()
        val connection = connectionGeneration
        val old = state.value
        val keep = next || (previous?.text == query.text && previous?.libraryId == query.libraryId)
        state.value = if (keep) old.copy(loading = true, error = null) else ListState(loading = true)
        listJobs[key] = viewModelScope.launch {
            try {
                val page = repository.assets(query, if (next) old.cursor else null)
                if (generation.isCurrent(ticket) && connection == connectionGeneration) {
                    state.value = ListState(if (next) (old.items + page.items).distinctBy(Asset::id) else page.items,
                        page.nextCursor, loaded = true)
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (generation.isCurrent(ticket) && connection == connectionGeneration)
                    state.value = state.value.copy(loading = false, error = explain(failure))
            }
        }
    }

    fun selectTab(index: Int) { saved["tab"] = index; saved["route"] = "main"; saved["backStack"] = arrayListOf<String>() }
    fun navigate(destination: String) {
        if (destination != route.value) saved["backStack"] = ArrayList(saved.get<ArrayList<String>>("backStack").orEmpty() + route.value)
        saved["route"] = destination
        mutableMessage.value = null
    }
    fun back() {
        detailGeneration.next(); detailJob?.cancel()
        val stack = saved.get<ArrayList<String>>("backStack").orEmpty()
        saved["route"] = stack.lastOrNull() ?: "main"
        saved["backStack"] = ArrayList(stack.dropLast(1))
    }

    fun refreshConnection() = action(blocking = false) {
        val caps = repository.capabilities()
        val libraries = repository.libraries()
        mutableCapabilities.value = caps
        mutableLibraries.value = libraries
        graph.uploads.schedule()
        if (uploadLibrary.value.isEmpty() && libraries.size == 1) setUploadLibrary(libraries.single().id)
        if (saved.get<Boolean>("sharedUpload") == true && queue.value?.drafts?.isNotEmpty() == true && uploadLibrary.value.isNotEmpty()) {
            graph.uploads.enqueue(uploadLibrary.value, uploadDirectory.value, UploadMode.ORDINARY)
            saved["sharedUpload"] = false
        }
    }

    fun login(address: String, token: String) = action { repository.login(address, token) }
    fun logout() = action { repository.logout(); saved["route"] = "main" }
    fun clearMessage() { mutableMessage.value = null }

    fun openAsset(id: String, sourceKey: String? = null) {
        if (sourceKey != null) saved["detailListKey"] = sourceKey
        navigate("asset:$id")
        mutableAsset.value = null
        detailJob?.cancel()
        val ticket = detailGeneration.next()
        val connection = connectionGeneration
        detailJob = viewModelScope.launch {
            try {
                val result = repository.asset(id)
                if (!detailGeneration.isCurrent(ticket) || connection != connectionGeneration) return@launch
                mutableAsset.value = result
                graph.uploads.event(result, "view")
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (detailGeneration.isCurrent(ticket)) mutableMessage.value = explain(failure) }
        }
    }

    fun adjacentAsset(direction: Int) {
        val id = mutableAsset.value?.id ?: return
        val source = saved.get<String>("detailListKey")?.let { lists[it]?.value?.items }.orEmpty()
        val index = source.indexOfFirst { it.id == id }
        val next = source.getOrNull(index + direction) ?: return
        // A sibling replaces the detail scene, without growing its return stack.
        saved["route"] = "asset:${next.id}"
        openAsset(next.id)
    }

    fun changeReaction(favorite: Boolean, liked: Boolean) = action {
        val current = mutableAsset.value ?: return@action
        publishAsset(repository.reaction(current, favorite, liked))
    }
    fun saveTags(tags: List<String>, onSaved: () -> Unit) = action {
        val current = mutableAsset.value ?: return@action
        publishAsset(repository.tags(current, tags)); onSaved()
    }
    fun saveNote(note: String, onSaved: () -> Unit) = action {
        val current = mutableAsset.value ?: return@action
        publishAsset(repository.updateNote(current, note)); onSaved()
    }
    fun loadTags() = action { mutableTags.value = repository.tags() }
    fun recordProgress(asset: Asset, position: Long) = action(blocking = false) { graph.uploads.event(asset, "progress", position) }
    fun mediaClient() = repository.mediaClient()
    private fun publishAsset(asset: Asset) {
        if (mutableAsset.value?.id == asset.id) mutableAsset.value = asset
        lists.values.forEach { state -> state.value = state.value.copy(items = state.value.items.map { if (it.id == asset.id) asset else it }) }
    }

    fun loadAuthors(next: Boolean = false) = loadPage(authorState, next) { repository.authors(it) }
    fun loadHistory(next: Boolean = false) = loadPage(historyState, next) { repository.history(it) }
    fun loadReports(next: Boolean = false) = loadPage(reportState, next) { repository.reports(it) }
    fun openAuthor(id: String) {
        navigate("author:$id"); mutableAuthor.value = null
        action { val result = repository.author(id); if (route.value == "author:$id") mutableAuthor.value = result }
    }
    fun followAuthor() = action { mutableAuthor.value?.let { mutableAuthor.value = repository.follow(it) } }
    fun editAuthor(name: String, aliases: List<String>, biography: String, homepage: String, onSaved: () -> Unit) = action {
        mutableAuthor.value?.let { mutableAuthor.value = repository.editAuthor(it, name, aliases, biography, homepage); onSaved() }
    }
    fun createLibrary(name: String, cos: Boolean, onSaved: () -> Unit) = action {
        repository.createLibrary(name, cos)
        mutableLibraries.value = repository.libraries()
        onSaved()
    }
    fun trashCurrentAsset() = action {
        mutableAsset.value?.let { current ->
            repository.trash(current)
            lists.values.forEach { it.value = it.value.copy(items = it.value.items.filterNot { item -> item.id == current.id }) }
            back()
        }
    }
    fun renameCurrentAsset(path: String, onSaved: () -> Unit) = action {
        mutableAsset.value?.let { publishAsset(repository.rename(it, path)); onSaved() }
    }
    fun loadTrash(next: Boolean = false) = loadPage(trashState, next) { repository.trashPage(it) }
    fun restoreAsset(asset: Asset) = action {
        repository.restore(asset)
        trashState.value = trashState.value.copy(items = trashState.value.items.filterNot { it.id == asset.id })
        lists.values.forEach { it.value = it.value.copy(loaded = false) }
    }
    fun openReport(id: String) {
        navigate("task:$id"); mutableReport.value = null
        action { val result = repository.report(id); if (route.value == "task:$id") mutableReport.value = result }
    }
    fun loadStatistics() = action { mutableStats.value = repository.statistics() }

    fun setUploadLibrary(id: String) {
        saved["uploadLibrary"] = id
        session.value?.let { graph.settings.saveUploadLibrary(it, id) }
    }
    fun setUploadDirectory(directory: String) { saved["uploadDirectory"] = directory }
    fun setUploadMode(mode: UploadMode) { saved["uploadMode"] = mode.name }
    fun receiveFiles(uris: List<Uri>, shared: Boolean = false) {
        navigate("upload")
        if (shared) saved["sharedUpload"] = true
        action {
            if (queue.value == null) graph.queue.load()
            graph.uploads.receive(uris)
            if (shared && session.value != null && libraries.value.any { it.id == uploadLibrary.value }) {
                graph.uploads.enqueue(uploadLibrary.value, uploadDirectory.value, UploadMode.ORDINARY)
                saved["sharedUpload"] = false
            }
        }
    }
    fun startUpload() = action { graph.uploads.enqueue(uploadLibrary.value, uploadDirectory.value, UploadMode.valueOf(uploadMode.value)) }
    fun renameDraft(id: String, name: String, onSaved: () -> Unit) = action { graph.uploads.renameDraft(id, name); onSaved() }
    fun removeDraft(id: String) = action { graph.uploads.discardDraft(id) }
    fun retryUpload(id: String) = action { graph.uploads.retry(id) }
    fun cancelUpload(id: String) = action { graph.uploads.cancel(id) }
    fun clearUploads() = action { graph.uploads.clearFinished() }
    fun reloadQueue() = action { graph.queue.load(); graph.uploads.schedule() }
    fun retryEvents() = action { graph.uploads.retryEvents() }
    fun savePreferences(preferences: Preferences) = action { graph.settings.savePreferences(preferences) }

    private fun <T> loadPage(state: MutableStateFlow<ListState<T>>, next: Boolean, loader: suspend (String?) -> Page<T>) {
        if (state.value.loading || (next && state.value.cursor == null)) return
        val old = state.value
        val generation = connectionGeneration
        state.value = old.copy(loading = true, error = null)
        viewModelScope.launch {
            try {
                val result = loader(if (next) old.cursor else null)
                if (generation == connectionGeneration) state.value = ListState(if (next) old.items + result.items else result.items, result.nextCursor, loaded = true)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (generation == connectionGeneration) state.value = old.copy(error = explain(failure)) }
        }
    }

    private fun action(blocking: Boolean = true, operation: suspend () -> Unit) {
        if (blocking && mutableBusy.value) return
        val generation = connectionGeneration
        if (blocking) mutableBusy.value = true
        mutableMessage.value = null
        val job = viewModelScope.launch(start = CoroutineStart.LAZY) {
            try { operation() }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (generation == connectionGeneration) mutableMessage.value = explain(failure) }
            finally { if (blocking && generation == connectionGeneration) mutableBusy.value = false }
        }
        activeOperations += job
        job.invokeOnCompletion { activeOperations.remove(job) }
        job.start()
    }
}

private fun explain(failure: Exception): String = when (failure) {
    is ApiFailure -> failure.message
    is java.io.IOException -> "连接失败，请检查服务器后重试"
    is IllegalArgumentException, is IllegalStateException -> failure.message ?: "操作未完成"
    else -> "操作未完成，请重试"
}
