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
import java.util.UUID

data class ListState<T>(val items: List<T> = emptyList(), val cursor: String? = null,
    val loading: Boolean = false, val loaded: Boolean = false, val error: String? = null)
data class LoadState<T>(val data: T? = null, val loading: Boolean = false, val error: String? = null)

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
    val uploadAuthorIds = saved.getStateFlow("uploadAuthorIds", emptyList<String>())
    val uploadCollectionId = saved.getStateFlow<String?>("uploadCollectionId", null)
    val uploadNote = saved.getStateFlow("uploadNote", "")
    val uploadSources = saved.getStateFlow("uploadSources", "")
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
    private val mutableAssetAuthors = MutableStateFlow<List<Author>>(emptyList())
    val assetAuthors = mutableAssetAuthors.asStateFlow()
    private val mutableReport = MutableStateFlow<TaskReport?>(null)
    val report = mutableReport.asStateFlow()
    private val mutableStats = MutableStateFlow<InventoryStats?>(null)
    val statistics = mutableStats.asStateFlow()
    private val analyticsState = MutableStateFlow(LoadState<AnalyticsReport>())
    val analytics = analyticsState.asStateFlow()
    private val reviewState = MutableStateFlow(LoadState<BrowsingReview>())
    val review = reviewState.asStateFlow()
    private val recommendationState = MutableStateFlow<RecommendationPreferences?>(null)
    val recommendationPreferences = recommendationState.asStateFlow()
    private var analyticsJob: Job? = null
    private var reviewJob: Job? = null
    private var rankingJob: Job? = null
    private var rankingQuery: String? = null
    private val rankingState = MutableStateFlow(ListState<ActivityRank>())
    val ranking = rankingState.asStateFlow()
    private val mutableTags = MutableStateFlow<List<String>>(emptyList())
    val tagVocabulary = mutableTags.asStateFlow()
    private val mutableTagCursor = MutableStateFlow<String?>(null)
    val tagCursor = mutableTagCursor.asStateFlow()
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
    private val authorTaskState = MutableStateFlow(ListState<AuthorTaskReport>())
    val authorTasks = authorTaskState.asStateFlow()
    private val trashState = MutableStateFlow(ListState<Asset>())
    val trash = trashState.asStateFlow()
    private val directoryState = MutableStateFlow(ListState<Directory>())
    val directories = directoryState.asStateFlow()
    private val collectionState = MutableStateFlow(ListState<MediaCollection>())
    val collections = collectionState.asStateFlow()
    private var directoryJob: Job? = null
    private var collectionJob: Job? = null
    private var directoryLocation = ""
    private var detailJob: Job? = null
    private var detailGeneration = RequestGeneration()
    private var connectionGeneration = 0L
    private val activeOperations = mutableSetOf<Job>()
    private val pageJobs = mutableMapOf<MutableStateFlow<*>, Job>()

    init {
        viewModelScope.launch {
            session.collect { connection ->
                connectionGeneration++
                activeOperations.toList().forEach(Job::cancel)
                mutableBusy.value = false
                listJobs.values.forEach(Job::cancel)
                pageJobs.values.forEach(Job::cancel); pageJobs.clear()
                detailJob?.cancel()
                detailGeneration.next()
                lists.values.forEach { it.value = ListState() }
                mutableAsset.value = null; mutableAuthor.value = null; mutableStats.value = null
                mutableAssetAuthors.value = emptyList()
                mutableTags.value = emptyList(); mutableTagCursor.value = null
                mutableLibraries.value = emptyList(); mutableCapabilities.value = null
                analyticsJob?.cancel(); reviewJob?.cancel(); directoryJob?.cancel(); collectionJob?.cancel(); rankingJob?.cancel()
                rankingState.value = ListState(); rankingQuery = null
                directoryState.value = ListState()
                collectionState.value = ListState()
                if (saved.get<String>("uploadInstance") != connection?.instanceId) {
                    saved["uploadAuthorIds"] = emptyList<String>(); saved["uploadCollectionId"] = null; saved["uploadSources"] = ""
                }
                saved["uploadInstance"] = connection?.instanceId
                analyticsState.value = LoadState(); reviewState.value = LoadState(); recommendationState.value = null
                authorState.value = ListState(); historyState.value = ListState(); reportState.value = ListState()
                authorTaskState.value = ListState()
                trashState.value = ListState()
                if (connection != null) {
                    saved["uploadLibrary"] = graph.settings.uploadLibrary(connection).orEmpty()
                    refreshConnection()
                }
            }
        }
    }

    fun assets(key: String): StateFlow<ListState<Asset>> = assetState(key)
    private fun assetState(key: String): MutableStateFlow<ListState<Asset>> {
        lists[key]?.let { return it }
        val dynamic = lists.keys.filter { it.startsWith("author:") || it.startsWith("collection:") }
        if ((key.startsWith("author:") || key.startsWith("collection:")) && dynamic.size >= 12) {
            val expired = dynamic.first()
            listJobs.remove(expired)?.cancel(); lists.remove(expired); queries.remove(expired); listVersions.remove(expired)
        }
        return MutableStateFlow(ListState<Asset>()).also { lists[key] = it }
    }
    fun query(key: String): AssetQuery = queries[key] ?: AssetQuery()
    fun browse(key: String, query: AssetQuery = query(key), next: Boolean = false, force: Boolean = false) {
        val state = assetState(key)
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
        if (uploadLibrary.value.isEmpty() && libraries.count { it.enabled } == 1) persistUploadLibrary(libraries.single { it.enabled }.id)
        if (saved.get<Boolean>("sharedUpload") == true && queue.value?.drafts?.isNotEmpty() == true && libraries.any { it.id == uploadLibrary.value && it.enabled }) {
            graph.uploads.enqueue(uploadLibrary.value, uploadDirectory.value, UploadMode.ORDINARY)
            saved["sharedUpload"] = false
        }
    }

    fun login(address: String, token: String) = action { repository.login(address, token) }
    fun logout() = action { repository.logout(); saved["route"] = "main" }
    fun disconnect() = action { repository.disconnect(); saved["route"] = "main" }
    fun clearMessage() { mutableMessage.value = null }

    fun openAsset(id: String, sourceKey: String? = null) {
        if (route.value != "asset:$id" || saved.get<String>("viewAsset") != id) {
            saved["viewSession"] = UUID.randomUUID().toString()
            saved["viewAsset"] = id
        }
        if (sourceKey != null) saved["detailListKey"] = sourceKey
        navigate("asset:$id")
        mutableAsset.value = null
        mutableAssetAuthors.value = emptyList()
        detailJob?.cancel()
        val ticket = detailGeneration.next()
        val connection = connectionGeneration
        detailJob = viewModelScope.launch {
            try {
                val result = repository.asset(id)
                if (!detailGeneration.isCurrent(ticket) || connection != connectionGeneration) return@launch
                mutableAsset.value = result
                val names = coroutineScope { result.authorIds.map { async { repository.author(it) } }.awaitAll() }
                if (detailGeneration.isCurrent(ticket) && connection == connectionGeneration) mutableAssetAuthors.value = names
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
    fun saveTags(tags: List<String>, expectedRevision: Long, onSaved: () -> Unit) = action {
        val current = mutableAsset.value ?: return@action
        publishAsset(repository.tags(current.copy(tagsRevision = expectedRevision), tags)); onSaved()
    }
    fun saveNote(note: String, onSaved: () -> Unit) = action {
        val current = mutableAsset.value ?: return@action
        publishAsset(repository.updateNote(current, note)); onSaved()
    }
    fun saveAuthors(ids: List<String>, onSaved: () -> Unit) = action {
        val current = mutableAsset.value ?: return@action
        val result = repository.updateAuthors(current, ids)
        publishAsset(result)
        mutableAssetAuthors.value = coroutineScope { result.authorIds.map { async { repository.author(it) } }.awaitAll() }
        onSaved()
    }
    fun saveRelations(sources: List<String>, characters: List<String>, onSaved: () -> Unit) = action {
        mutableAsset.value?.let { publishAsset(repository.updateRelations(it, sources, characters)); onSaved() }
    }
    fun saveAcquisitionSources(sources: List<String>, onSaved: () -> Unit) = action {
        mutableAsset.value?.let { publishAsset(repository.updateAcquisitionSources(it, sources)); onSaved() }
    }
    fun loadTags(next: Boolean = false) = action {
        val page = repository.tags(if (next) mutableTagCursor.value else null)
        mutableTags.value = if (next) (mutableTags.value + page.items).distinct() else page.items
        mutableTagCursor.value = page.nextCursor
    }
    fun removeTag(tag: String, onSaved: (Asset) -> Unit) = action {
        mutableAsset.value?.let { val result = repository.removeTag(it, tag); publishAsset(result); onSaved(result) }
    }
    fun createTag(name: String, onSaved: (String) -> Unit) = action { onSaved(repository.createTag(name)) }
    fun refreshTagsState() = action {
        mutableAsset.value?.let { publishAsset(repository.asset(it.id)) }
        val page = repository.tags()
        mutableTags.value = page.items; mutableTagCursor.value = page.nextCursor
    }
    fun viewSessionId(): String = saved.get<String>("viewSession") ?: UUID.randomUUID().toString().also { saved["viewSession"] = it }
    fun recordView(asset: Asset, sessionId: String) = graph.recordEvent(asset, "view", sessionId)
    fun recordProgress(asset: Asset, position: Long, sessionId: String) = graph.recordEvent(asset, "progress", sessionId, positionMs = position)
    fun recordPlay(asset: Asset, sessionId: String) = graph.recordEvent(asset, "play", sessionId)
    fun recordDwell(asset: Asset, sessionId: String, seconds: Double, startedAt: String) {
        if (seconds >= 1.0) graph.recordEvent(asset, "dwell", sessionId, seconds = seconds, occurredAt = startedAt)
    }
    fun mediaClient(target: Session) = repository.mediaClient(target)
    private fun publishAsset(asset: Asset) {
        if (mutableAsset.value?.id == asset.id) mutableAsset.value = asset
        lists.values.forEach { state -> state.value = state.value.copy(items = state.value.items.map { if (it.id == asset.id) asset else it }) }
    }

    fun loadAuthors(next: Boolean = false) = loadPage(authorState, next) { repository.authors(it) }
    fun loadHistory(next: Boolean = false) = loadPage(historyState, next) { repository.history(it) }
    fun loadReports(next: Boolean = false) = loadPage(reportState, next) { repository.reports(it) }
    fun loadAuthorTasks(next: Boolean = false) = loadPage(authorTaskState, next) { repository.authorTasks(it) }
    fun cancelAuthorTask(id: String) = action { repository.cancelAuthorTask(id); loadAuthorTasks() }
    fun openAuthor(id: String) {
        navigate("author:$id"); mutableAuthor.value = null
        action { val result = repository.author(id); if (route.value == "author:$id") mutableAuthor.value = result }
    }
    fun followAuthor() = action { mutableAuthor.value?.let { mutableAuthor.value = repository.follow(it) } }
    fun editAuthor(name: String, aliases: List<String>, biography: String, homepage: String, onSaved: () -> Unit) = action {
        mutableAuthor.value?.let { mutableAuthor.value = repository.editAuthor(it, name, aliases, biography, homepage); onSaved() }
    }
    fun createAuthor(name: String, onSaved: () -> Unit) = action { repository.createAuthor(name); onSaved(); loadAuthors() }
    fun deleteCurrentAuthor() = action {
        mutableAuthor.value?.let { repository.deleteAuthor(it); back(); loadAuthors() }
    }
    fun mergeCurrentAuthor(target: Author, onSaved: () -> Unit) = action {
        mutableAuthor.value?.let {
            val result = repository.mergeAuthors(it, target)
            mutableAuthor.value = result.first
            saved["route"] = "author:${result.first.id}"
            mutableMessage.value = result.second
            onSaved(); loadAuthors()
        }
    }
    fun createLibrary(name: String, cos: Boolean, onSaved: () -> Unit) = action {
        repository.createLibrary(name, cos)
        mutableLibraries.value = repository.libraries()
        onSaved()
    }
    fun updateLibrary(library: Library, name: String = library.name, enabled: Boolean = library.enabled) = action {
        val updated = repository.updateLibrary(library, name, enabled)
        mutableLibraries.value = mutableLibraries.value.map { if (it.id == updated.id) updated else it }
    }
    fun loadDirectories(libraryId: String, parent: String, next: Boolean = false) {
        if (next && (directoryState.value.loading || directoryState.value.cursor == null)) return
        val location = "$libraryId:$parent"
        val previous = directoryState.value
        directoryLocation = location
        directoryJob?.cancel()
        directoryState.value = if (next) previous.copy(loading = true, error = null) else ListState(loading = true)
        val connection = connectionGeneration
        directoryJob = viewModelScope.launch {
            try {
                val page = repository.directories(libraryId, parent, if (next) previous.cursor else null)
                if (connection == connectionGeneration && directoryLocation == location)
                    directoryState.value = ListState(if (next) previous.items + page.items else page.items, page.nextCursor, loaded = true)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (connection == connectionGeneration && directoryLocation == location) directoryState.value = ListState(error = explain(failure)) }
        }
    }
    fun createDirectory(libraryId: String, parent: String, name: String, onSaved: () -> Unit) = action {
        require(name.isNotBlank() && '/' !in name && '\\' !in name) { "请输入单个目录名称" }
        repository.createDirectory(libraryId, listOf(parent, name.trim()).filter(String::isNotEmpty).joinToString("/"))
        onSaved(); loadDirectories(libraryId, parent)
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
    fun purgeAsset(asset: Asset) = action {
        repository.purge(asset)
        trashState.value = trashState.value.copy(items = trashState.value.items.filterNot { it.id == asset.id })
    }
    fun openReport(id: String) {
        navigate("task:$id"); mutableReport.value = null
        action { val result = repository.report(id); if (route.value == "task:$id") mutableReport.value = result }
    }
    fun cancelReport() = action { mutableReport.value?.let { mutableReport.value = repository.cancelReport(it) } }
    fun loadStatistics() = action { mutableStats.value = repository.statistics() }
    fun loadAnalytics(range: String) {
        analyticsJob?.cancel()
        analyticsState.value = LoadState(loading = true)
        val connection = connectionGeneration
        analyticsJob = viewModelScope.launch {
            try { val result = graph.analytics.report(range); if (connection == connectionGeneration) analyticsState.value = LoadState(data = result) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (connection == connectionGeneration) analyticsState.value = LoadState(error = explain(failure)) }
        }
    }
    fun loadReview(range: String) {
        reviewJob?.cancel()
        reviewState.value = LoadState(loading = true)
        val connection = connectionGeneration
        reviewJob = viewModelScope.launch {
            try { val result = graph.analytics.review(range); if (connection == connectionGeneration) reviewState.value = LoadState(data = result) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (connection == connectionGeneration) reviewState.value = LoadState(error = explain(failure)) }
        }
    }
    fun loadRecommendationPreferences() = action { recommendationState.value = graph.analytics.preferences() }
    fun loadRanking(kind: String, range: String, metric: String, next: Boolean = false) {
        val query = "$kind:$range:$metric"
        if (next && (rankingState.value.loading || rankingState.value.cursor == null || rankingQuery != query)) return
        rankingJob?.cancel()
        val previous = if (rankingQuery == query) rankingState.value else ListState<ActivityRank>()
        rankingQuery = query
        rankingState.value = previous.copy(loading = true, error = null)
        val connection = connectionGeneration
        rankingJob = viewModelScope.launch {
            try {
                val result = graph.analytics.ranking(kind, range, metric, if (next) previous.cursor else null)
                if (rankingQuery == query && connection == connectionGeneration)
                    rankingState.value = ListState(if (next) previous.items + result.items else result.items, result.nextCursor, loaded = true)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (rankingQuery == query && connection == connectionGeneration) rankingState.value = previous.copy(error = explain(failure)) }
        }
    }
    fun chooseRecommendationPreset(preset: RecommendationWeights) = action {
        recommendationState.value?.let { recommendationState.value = graph.analytics.savePreferences(it, preset) }
        lists.filterKeys { it.startsWith("home:") }.values.forEach { it.value = it.value.copy(loaded = false) }
    }

    fun setUploadLibrary(id: String) = action(blocking = false) { persistUploadLibrary(id) }
    private suspend fun persistUploadLibrary(id: String) {
        session.value?.let { target -> withContext(Dispatchers.IO) { graph.settings.saveUploadLibrary(target, id) } }
        if (id != uploadLibrary.value) {
            saved["uploadAuthorIds"] = emptyList<String>(); saved["uploadCollectionId"] = null; saved["uploadSources"] = ""
            collectionJob?.cancel(); collectionState.value = ListState()
        }
        saved["uploadLibrary"] = id
    }
    fun setUploadDirectory(directory: String) { saved["uploadDirectory"] = directory }
    fun setUploadMode(mode: UploadMode) { saved["uploadMode"] = mode.name }
    fun setUploadAuthors(ids: List<String>) { saved["uploadAuthorIds"] = ids; saved["uploadSources"] = "" }
    fun setUploadCollection(id: String?) { saved["uploadCollectionId"] = id }
    fun setUploadNote(note: String) { saved["uploadNote"] = note }
    fun setUploadSources(sources: String) { saved["uploadSources"] = sources }
    fun loadCollections(next: Boolean = false) {
        if (next && (collectionState.value.loading || collectionState.value.cursor == null)) return
        collectionJob?.cancel()
        val library = uploadLibrary.value
        val previous = collectionState.value.copy(loading = false)
        val connection = connectionGeneration
        collectionState.value = previous.copy(loading = true, error = null)
        collectionJob = viewModelScope.launch {
            try {
                val page = repository.collections(library, if (next) previous.cursor else null)
                if (library == uploadLibrary.value && connection == connectionGeneration)
                    collectionState.value = ListState(if (next) previous.items + page.items else page.items, page.nextCursor, loaded = true)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (library == uploadLibrary.value && connection == connectionGeneration) collectionState.value = previous.copy(error = explain(failure))
            }
        }
    }
    fun createCollection(name: String, kind: String, onSaved: () -> Unit) = action {
        repository.createCollection(uploadLibrary.value, name, kind); onSaved(); loadCollections()
    }
    fun receiveFiles(uris: List<Uri>, shared: Boolean = false) {
        navigate("upload")
        if (shared) saved["sharedUpload"] = true
        action {
            if (queue.value == null) graph.queue.load()
            graph.uploads.receive(uris)
            if (shared && session.value != null && libraries.value.any { it.id == uploadLibrary.value && it.enabled }) {
                graph.uploads.enqueue(uploadLibrary.value, uploadDirectory.value, UploadMode.ORDINARY)
                saved["sharedUpload"] = false
            }
        }
    }
    fun startUpload() = action {
        check(libraries.value.any { it.id == uploadLibrary.value && it.enabled }) { "请选择正在接收上传的影库" }
        graph.uploads.enqueue(uploadLibrary.value, uploadDirectory.value, UploadMode.valueOf(uploadMode.value),
            UploadOptions(uploadAuthorIds.value, uploadCollectionId.value, uploadNote.value, uploadSources.value.lines().filter(String::isNotBlank)))
    }
    fun renameDraft(id: String, name: String, onSaved: () -> Unit) = action { graph.uploads.renameDraft(id, name); onSaved() }
    fun removeDraft(id: String) = action { graph.uploads.discardDraft(id) }
    fun retryUpload(id: String) = action { graph.uploads.retry(id) }
    fun cancelUpload(id: String) = action { graph.uploads.cancel(id) }
    fun clearUploads() = action { graph.uploads.clearFinished() }
    fun reloadQueue() = action { graph.queue.load(); graph.uploads.schedule() }
    fun retryEvents() = action { graph.uploads.retryEvents() }
    fun savePreferences(preferences: Preferences) = action { withContext(Dispatchers.IO) { graph.settings.savePreferences(preferences) } }

    private fun <T> loadPage(state: MutableStateFlow<ListState<T>>, next: Boolean, loader: suspend (String?) -> Page<T>) {
        if (next && (state.value.loading || state.value.cursor == null)) return
        pageJobs.remove(state)?.cancel()
        val old = state.value.copy(loading = false)
        val generation = connectionGeneration
        state.value = old.copy(loading = true, error = null)
        pageJobs[state] = viewModelScope.launch {
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
        if (blocking) mutableMessage.value = null
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
