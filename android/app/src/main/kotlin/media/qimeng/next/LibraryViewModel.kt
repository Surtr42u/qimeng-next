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
    val loading: Boolean = false, val loaded: Boolean = false, val error: String? = null,
    val totalMatched: Long? = null, val dateCounts: List<DateCount> = emptyList(), val loadedQuery: AssetQuery? = null,
    val unknownDateCount: Long? = null, val reloadFromStart: Boolean = false, val publicationId: String? = null)
data class LoadState<T>(val data: T? = null, val loading: Boolean = false, val error: String? = null)
private data class ReactionIntent(val target: Session, val id: String, val revision: Long, val enabled: Boolean)

class LibraryViewModel(application: Application, private val saved: SavedStateHandle) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    private val runtime = (application as QimengApplication).runtime
    private val localConnection = (application as QimengApplication).localConnection
    val localRuntimeAvailable = runtime.available
    val localRuntimeStatus = runtime.status
    val localAddress = runtime.configuredAddress
    val localFileAccess = runtime.files.access
    val localScanState = runtime.scans.state
    val defaultEndpoint = graph.settings.defaultEndpoint
    val discovery = graph.discovery.state
    val stagedLoginEndpoint = saved.getStateFlow<String?>("loginEndpoint", null)
    val stagedLoginAddress = saved.getStateFlow<String?>("loginAddress", null)
    val recentServers = graph.settings.recentServers
    val transportRevision = graph.settings.transportRevision
    val localConnectionError = localConnection.error
    private val repository = graph.media
    val session = repository.session
    val preferences = graph.settings.preferences
    val settingsError = graph.settings.error
    val queue = graph.queue.state
    val lastPublication = graph.uploads.lastPublication
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
    val uploadAuthorSources = saved.getStateFlow("uploadAuthorSources", "")
    val uploadFamily = saved.getStateFlow("uploadFamily", "")
    val uploadAutoName = saved.getStateFlow("uploadAutoName", false)
    val editedStems = saved.getStateFlow("uploadStems", HashMap<String, String>())
    private val nameWrites = mutableMapOf<String, Job>()
    private val mutableMessage = MutableStateFlow<String?>(null)
    val message = mutableMessage.asStateFlow()
    private val mutableBusy = MutableStateFlow(false)
    val busy = mutableBusy.asStateFlow()
    private val libraryState = MutableStateFlow(ListState<Library>())
    val libraryPage = libraryState.asStateFlow()
    val libraries = libraryState.map { it.items }.stateIn(viewModelScope, SharingStarted.Eagerly, emptyList())
    private val mutableCapabilities = MutableStateFlow<ServerCapabilities?>(null)
    val capabilities = mutableCapabilities.asStateFlow()
    private val mutableAsset = MutableStateFlow<Asset?>(null)
    private val reactionRetries = mutableMapOf<String, Pair<ReactionIntent, String>>()
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
    private val tagReads = RequestGeneration()
    private val lists = mutableMapOf<String, MutableStateFlow<ListState<Asset>>>()
    private val queries = mutableMapOf<String, AssetQuery>()
    private val listJobs = mutableMapOf<String, Job>()
    private val listVersions = mutableMapOf<String, RequestGeneration>()
    private val authorState = MutableStateFlow(ListState<Author>())
    val authors = authorState.asStateFlow()
    private val reportState = MutableStateFlow(ListState<TaskReport>())
    val reports = reportState.asStateFlow()
    private val authorTaskState = MutableStateFlow(ListState<AuthorTaskReport>())
    val authorTasks = authorTaskState.asStateFlow()
    private val trashState = MutableStateFlow(ListState<Asset>())
    val trash = trashState.asStateFlow()
    private val collectionState = MutableStateFlow(ListState<MediaCollection>())
    val collections = collectionState.asStateFlow()
    private var collectionJob: Job? = null
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
                reactionRetries.clear()
                lists.values.forEach { it.value = ListState() }
                mutableAsset.value = null; mutableAuthor.value = null; mutableStats.value = null; mutableReport.value = null
                discardForeignObjectRoutes(connection)
                mutableAssetAuthors.value = emptyList()
                tagReads.next()
                mutableTags.value = emptyList(); mutableTagCursor.value = null
                libraryState.value = ListState(); mutableCapabilities.value = null
                analyticsJob?.cancel(); reviewJob?.cancel(); collectionJob?.cancel(); rankingJob?.cancel()
                rankingState.value = ListState(); rankingQuery = null
                collectionState.value = ListState()
                if (saved.get<String>("uploadInstance") != connection?.instanceId) {
                    saved["uploadAuthorIds"] = emptyList<String>(); saved["uploadCollectionId"] = null; saved["uploadSources"] = ""; saved["uploadFamily"] = ""; saved["uploadAutoName"] = false
                    saved["uploadDirectory"] = ""; saved["uploadNote"] = ""; saved["uploadAuthorSources"] = ""
                }
                saved["uploadInstance"] = connection?.instanceId
                analyticsState.value = LoadState(); reviewState.value = LoadState(); recommendationState.value = null
                authorState.value = ListState(); reportState.value = ListState()
                authorTaskState.value = ListState()
                trashState.value = ListState()
                if (connection != null) {
                    saved["loginEndpoint"] = null; saved["loginAddress"] = null; saved["loginLocalAddress"] = null
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
    fun cancelBrowse(key: String) {
        listJobs.remove(key)?.cancel()
        listVersions[key]?.next()
        lists[key]?.let { state ->
            if (state.value.loading) state.value = state.value.copy(loading = false, loaded = false)
        }
    }
    fun browse(key: String, query: AssetQuery = query(key), next: Boolean = false, force: Boolean = false) {
        val state = assetState(key)
        val previous = queries[key]
        val publication = lastPublication.value?.takeIf { sameTarget(it.first, session.value) }?.second
        if (next && (state.value.loading || state.value.cursor == null || state.value.loadedQuery != query || state.value.reloadFromStart || state.value.publicationId != publication)) return
        if (!next && !force && previous == query && state.value.publicationId == publication && (state.value.loaded || state.value.loading)) return
        queries[key] = query
        listJobs[key]?.cancel()
        val generation = listVersions.getOrPut(key) { RequestGeneration() }
        val ticket = generation.next()
        val connection = connectionGeneration
        val old = state.value
        val keep = next || previous == query
        state.value = if (keep) old.copy(loading = true, error = null, publicationId = publication) else ListState(loading = true, publicationId = publication)
        listJobs[key] = viewModelScope.launch {
            try {
                val page = repository.assets(query, if (next) old.cursor else null)
                if (generation.isCurrent(ticket) && connection == connectionGeneration) {
                    state.value = ListState(if (next) (old.items + page.items).distinctBy(Asset::id) else page.items,
                        page.nextCursor, loaded = true, totalMatched = page.totalMatched,
                        dateCounts = if (next) old.dateCounts else page.dateCounts, loadedQuery = query,
                        unknownDateCount = if (next) old.unknownDateCount else page.unknownDateCount, publicationId = publication)
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (generation.isCurrent(ticket) && connection == connectionGeneration)
                    state.value = state.value.copy(loading = false, error = explain(failure),
                        reloadFromStart = next && failure is ApiFailure && failure.code == "recommendation_expired")
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
        mutableCapabilities.value = caps
        loadLibraries()
        graph.uploads.schedule()
        session.value?.let { graph.archives.resume(it) }
    }
    fun loadLibraries(next: Boolean = false) = loadPage(libraryState, next) { cursor ->
        val page = repository.libraryPage(cursor)
        if (cursor != null) {
            val visible = libraryState.value.items.mapTo(mutableSetOf(), Library::id)
            return@loadPage page.copy(items = page.items.filterNot { it.id in visible })
        }
        if (uploadLibrary.value.isEmpty() && page.nextCursor == null && page.items.count { it.enabled } == 1) {
            persistUploadLibrary(page.items.single { it.enabled }.id)
        }
        val target = session.value
        val selected = uploadLibrary.value
        if (target != null && selected.isNotEmpty() && page.nextCursor != null && page.items.none { it.id == selected }) {
            val savedLibrary = try { repository.library(target, selected) } catch (failure: ApiFailure) {
                if (failure.status != 404) throw failure
                null
            }
            if (savedLibrary != null) return@loadPage page.copy(items = listOf(savedLibrary) + page.items)
        }
        page
    }

    fun login(address: String, password: String, expectedInstanceId: String? = null) = action {
        repository.loginWithPassword(address, password, expectedInstanceId = expectedInstanceId, loginEndpoint = "nas")
    }
    fun loginLocal(password: String = "") = action {
        saved.get<String>("loginLocalAddress")?.let { runtime.configureAddress(it) }
        connectLocal(forceLogin = true, password = password)
    }
    fun discoverServers() = graph.discovery.start(graph.settings.recentServers.value)
    fun stopDiscovery() = graph.discovery.stop()
    fun requestLocalFileAccess() = action { runtime.files.requestAccess() }
    fun refreshLocalFileAccess() = runtime.files.refreshAccess()
    fun isLocalSession() = session.value?.let { runtime.available && it.baseUrl in runtime.ownedEndpoints(it.instanceId) } == true
    fun setChargeOnlyScan(enabled: Boolean) = action { runtime.scans.setChargeOnly(enabled) }
    fun saveDefaultEndpoint(value: String) = action(blocking = false) {
        check(value != "local" || runtime.available) { "此安装包未包含本机服务" }
        withContext(Dispatchers.IO) { graph.settings.saveDefaultEndpoint(value) }
    }
    fun prepareLogin(endpoint: String, address: String? = null) = action {
        val normalized = if (endpoint == "local") runtime.validateAddress(address ?: runtime.configuredAddress.value)
            else address?.takeIf { it.isNotBlank() }?.let { media.qimeng.next.data.normalizedServer(it) }
        withContext(Dispatchers.IO) { graph.settings.saveDefaultEndpoint(endpoint) }
        saved["loginEndpoint"] = endpoint
        if (endpoint == "local") saved["loginLocalAddress"] = normalized else saved["loginAddress"] = normalized
        saved["route"] = "main"; saved["backStack"] = ArrayList<String>()
        repository.disconnect()
    }
    fun stopLocal() = action { runtime.stop() }
    fun restoreLocalConnection() {
        runtime.files.refreshAccess()
        session.value?.let { graph.backups.resume(it) }
        if (runtime.available && runtime.shouldRestore && session.value?.baseUrl == runtime.baseUrl) action { connectLocal(forceLogin = false) }
    }
    private suspend fun connectLocal(forceLogin: Boolean, password: String = "") {
        localConnection.connect(forceLogin, password)
        refreshConnection()
    }
    fun logout() = action { repository.logout(); saved["route"] = "main" }
    fun disconnect() = action { repository.disconnect(); saved["route"] = "main" }
    fun clearMessage() { mutableMessage.value = null }

    private fun ownsObjectRoutes(target: Session) =
        saved.get<String>("objectInstance") == target.instanceId && saved.get<String>("objectEndpoint") == target.baseUrl

    private fun discardForeignObjectRoutes(target: Session?) {
        if (target != null && ownsObjectRoutes(target)) return
        fun objectRoute(value: String) = value.startsWith("asset:") || value.startsWith("task:")
        val stack = saved.get<ArrayList<String>>("backStack").orEmpty()
        val retained = stack.filterNot(::objectRoute)
        if (objectRoute(route.value)) {
            saved["route"] = retained.lastOrNull() ?: "main"
            saved["backStack"] = ArrayList(retained.dropLast(1))
        } else if (retained.size != stack.size) saved["backStack"] = ArrayList(retained)
    }

    private fun bindObjectTarget(target: Session): Boolean {
        if (!sameTarget(target, session.value)) {
            mutableMessage.value = "请先连接任务原来的服务器"
            return false
        }
        discardForeignObjectRoutes(target)
        saved["objectInstance"] = target.instanceId
        saved["objectEndpoint"] = target.baseUrl
        return true
    }

    private fun targetForRefresh(destination: String) = session.value?.takeIf { route.value == destination && ownsObjectRoutes(it) }

    fun refreshAsset(id: String) { targetForRefresh("asset:$id")?.let { openAsset(id, target = it) } }

    fun openAsset(id: String, sourceKey: String? = null, target: Session? = session.value) {
        if (target == null || !bindObjectTarget(target)) return
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
                val result = repository.asset(id, target)
                if (!detailGeneration.isCurrent(ticket) || connection != connectionGeneration || !sameTarget(target, session.value)) return@launch
                mutableAsset.value = result
                val names = coroutineScope { result.authorIds.map { async { repository.author(it, target) } }.awaitAll() }
                if (detailGeneration.isCurrent(ticket) && connection == connectionGeneration && sameTarget(target, session.value)) mutableAssetAuthors.value = names
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (detailGeneration.isCurrent(ticket) && sameTarget(target, session.value)) mutableMessage.value = explain(failure) }
        }
    }

    fun adjacentAsset(direction: Int) {
        val id = mutableAsset.value?.id ?: return
        val source = saved.get<String>("detailListKey")?.let { lists[it]?.value?.items }.orEmpty()
        val index = source.indexOfFirst { it.id == id }
        val next = source.getOrNull(index + direction) ?: return
        // A sibling replaces the detail scene, without growing its return stack.
        saved["route"] = "asset:${next.id}"
        openAsset(next.id, target = next.target)
    }

    fun detailPosition(id: String): Pair<Int, Int> {
        val batch = saved.get<String>("detailListKey")?.let { lists[it]?.value?.items }.orEmpty()
        return batch.indexOfFirst { it.id == id } to batch.size
    }

    fun setFavorite(asset: Asset, favorite: Boolean) = applyReaction("favorite", asset, favorite) { key -> repository.setFavorite(asset, favorite, key) }
    fun setTodayLike(asset: Asset, liked: Boolean) = applyReaction("like", asset, liked) { key -> repository.setTodayLike(asset, liked, key) }

    private fun applyReaction(field: String, asset: Asset, enabled: Boolean, send: suspend (String) -> Asset) = action {
        if (!sameTarget(asset.target, session.value)) return@action
        val intent = ReactionIntent(asset.target, asset.id, asset.revision, enabled)
        val key = reactionRetries[field]?.takeIf { it.first == intent }?.second ?: UUID.randomUUID().toString()
        // An ambiguous response leaves the key attached to this exact gesture on the displayed revision.
        reactionRetries[field] = intent to key
        val updated = try { send(key) } catch (failure: ApiFailure) {
            if (failure.status in 400..499 && failure.status !in setOf(408, 429) && reactionRetries[field]?.second == key) reactionRetries.remove(field)
            throw failure
        }
        if (reactionRetries[field]?.second == key) reactionRetries.remove(field)
        publishAsset(updated)
        if (!sameTarget(updated.target, session.value)) return@action
        queries.filterValues { it.favorite != null || it.liked != null }.keys.forEach { name ->
            cancelBrowse(name)
            lists[name]?.let { it.value = it.value.copy(loaded = false) }
        }
    }
    fun saveTags(tags: List<String>, expectedRevision: Long, onSaved: () -> Unit) = action {
        val current = mutableAsset.value ?: return@action
        publishAsset(repository.tags(current.copy(tagsRevision = expectedRevision), tags)); onSaved()
    }
    fun saveNote(asset: Asset, note: String, onSaved: () -> Unit) = action {
        publishAsset(repository.updateNote(asset, note)); onSaved()
    }
    fun saveAuthors(asset: Asset, ids: List<String>, names: List<String>, revision: Long, authorsRevision: Long, onSaved: () -> Unit) = action {
        val result = repository.updateAuthors(asset, ids, names, revision, authorsRevision)
        publishAsset(result)
        mutableAssetAuthors.value = coroutineScope { result.authorIds.map { async { repository.author(it, result.target) } }.awaitAll() }
        onSaved()
    }
    fun saveRelations(asset: Asset, sources: List<String>, characters: List<String>, onSaved: () -> Unit) = action {
        publishAsset(repository.updateRelations(asset, sources, characters)); onSaved()
    }
    fun saveAcquisitionSources(asset: Asset, sources: List<String>, onSaved: () -> Unit) = action {
        publishAsset(repository.updateAcquisitionSources(asset, sources)); onSaved()
    }
    fun loadTags(next: Boolean = false) = action(blocking = false) { readTags(next) }

    private suspend fun readTags(next: Boolean) {
        val cursor = if (next) (mutableTagCursor.value ?: return) else null
        val ticket = tagReads.next()
        val page = repository.tags(cursor)
        if (!tagReads.isCurrent(ticket)) return
        mutableTags.value = if (next) (mutableTags.value + page.items).distinct() else page.items
        mutableTagCursor.value = page.nextCursor
    }
    fun removeTag(tag: String, onSaved: (Asset) -> Unit) = action {
        mutableAsset.value?.let { val result = repository.removeTag(it, tag); publishAsset(result); onSaved(result) }
    }
    fun createTag(name: String, onSaved: (String) -> Unit) = action { onSaved(repository.createTag(name)) }
    fun refreshTagsState() = action {
        mutableAsset.value?.let { publishAsset(repository.asset(it.id, it.target)) }
        readTags(false)
    }
    fun viewSessionId(): String = saved.get<String>("viewSession") ?: UUID.randomUUID().toString().also { saved["viewSession"] = it }
    fun recordView(asset: Asset, sessionId: String) = graph.recordEvent(asset, "view", sessionId)
    fun recordProgress(asset: Asset, position: Long, sessionId: String) = graph.recordEvent(asset, "progress", sessionId, positionMs = position)
    fun recordPlay(asset: Asset, sessionId: String) = graph.recordEvent(asset, "play", sessionId)
    fun recordDwell(asset: Asset, sessionId: String, seconds: Double, startedAt: String) {
        if (seconds >= 1.0) graph.recordEvent(asset, "dwell", sessionId, seconds = seconds, occurredAt = startedAt)
    }
    fun mediaClient(target: Session) = repository.mediaClient(target)
    fun addThumbnailImageComponents(builder: coil3.ComponentRegistry.Builder) = graph.thumbnails.addImageComponents(builder)
    private fun publishAsset(asset: Asset) {
        if (!sameTarget(asset.target, session.value)) return
        if (mutableAsset.value?.let { it.id == asset.id && it.revision <= asset.revision } == true) mutableAsset.value = asset
        lists.values.forEach { state -> state.value = state.value.copy(items = state.value.items.map {
            if (it.id == asset.id && it.revision <= asset.revision) asset.copy(rankingScore = it.rankingScore, lastViewedAt = it.lastViewedAt) else it
        }) }
    }

    fun loadAuthors(next: Boolean = false) = loadPage(authorState, next) { repository.authors(it) }
    fun loadReports(next: Boolean = false) = loadPage(reportState, next) { repository.reports(it) }
    fun loadAuthorTasks(next: Boolean = false) = loadPage(authorTaskState, next) { repository.authorTasks(it) }
    fun cancelAuthorTask(id: String) = action { repository.cancelAuthorTask(id); loadAuthorTasks() }
    fun openAuthor(id: String) {
        navigate("author:$id"); mutableAuthor.value = null
        action { val result = repository.author(id); if (route.value == "author:$id") mutableAuthor.value = result }
    }
    fun followAuthor() = action { mutableAuthor.value?.let { mutableAuthor.value = repository.follow(it) } }
    fun followAssetAuthor(author: Author) = action {
        val updated = repository.follow(author)
        mutableAssetAuthors.value = mutableAssetAuthors.value.map { if (it.id == author.id) updated else it }
        if (mutableAuthor.value?.id == author.id) mutableAuthor.value = updated
    }
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
    fun createDirectory(libraryId: String, parent: String, name: String, onSaved: () -> Unit) = action {
        require(name.isNotBlank() && '/' !in name && '\\' !in name) { "请输入单个目录名称" }
        repository.createDirectory(libraryId, listOf(parent, name.trim()).filter(String::isNotEmpty).joinToString("/"))
        onSaved()
    }
    fun trashCurrentAsset(asset: Asset) = action {
        repository.trash(asset)
        lists.values.forEach { it.value = it.value.copy(loaded = false, items = it.value.items.filterNot { item -> item.id == asset.id }) }
        if (route.value == "asset:${asset.id}") back()
    }
    fun renameCurrentAsset(asset: Asset, path: String, onSaved: () -> Unit) = action {
        publishAsset(repository.rename(asset, path)); onSaved()
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
    fun refreshReport(id: String) { targetForRefresh("task:$id")?.let { openReport(id, it) } }

    fun openReport(id: String, target: Session? = session.value) {
        if (target == null || !bindObjectTarget(target)) return
        navigate("task:$id"); mutableReport.value = null
        action {
            val result = repository.report(id, target)
            if (route.value == "task:$id" && sameTarget(target, session.value)) mutableReport.value = result
        }
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
            saved["uploadAuthorIds"] = emptyList<String>(); saved["uploadCollectionId"] = null; saved["uploadSources"] = ""; saved["uploadFamily"] = ""; saved["uploadAutoName"] = false
            saved["uploadDirectory"] = ""; saved["uploadAuthorSources"] = ""
            collectionJob?.cancel(); collectionState.value = ListState()
        }
        saved["uploadLibrary"] = id
    }
    fun setUploadDirectory(directory: String) { saved["uploadDirectory"] = directory }
    fun setUploadMode(mode: UploadMode) { saved["uploadMode"] = mode.name }
    fun setUploadAuthors(ids: List<String>) {
        if (ids != uploadAuthorIds.value) saved["uploadAuthorSources"] = ""
        saved["uploadAuthorIds"] = ids
    }
    fun setUploadCollection(id: String?) { saved["uploadCollectionId"] = id }
    fun setUploadNote(note: String) { saved["uploadNote"] = note }
    fun setUploadSources(sources: String) { saved["uploadSources"] = sources }
    fun setUploadAuthorSources(sources: String) { saved["uploadAuthorSources"] = sources }
    fun setUploadFamily(family: String) { saved["uploadFamily"] = family }
    fun setUploadAutoName(enabled: Boolean) { saved["uploadAutoName"] = enabled }
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
        val target = session.value
        val destination = if (shared && target != null) graph.settings.uploadLibrary(target) else null
        val directory = if (destination == uploadLibrary.value && saved.get<String>("uploadInstance") == target?.instanceId) uploadDirectory.value else ""
        // Transient Android URI grants must be consumed even when another UI action is busy or a login changes.
        viewModelScope.launch {
            try {
                graph.awaitRecovery()
                val group = if (target != null && !destination.isNullOrEmpty()) "share-${UUID.randomUUID()}" else null
                try {
                    val inputs = graph.uploads.receive(uris, intakeGroup = group)
                    if (group == null) return@launch
                    if (target != null && destination != null && sameTarget(target, session.value) && repository.library(target, destination).enabled) {
                        graph.uploads.enqueue(destination, directory, UploadMode.ORDINARY, target = target, inputIds = inputs, batchKey = group)
                    } else mutableMessage.value = "原上传位置已改变，本次分享已保留，请选择上传位置"
                } finally {
                    if (group != null) withContext(NonCancellable) { graph.uploads.releaseSharedDrafts(group) }
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutableMessage.value = explain(failure) }
        }
    }
    fun startUpload() = action {
        val target = session.value ?: error("请先连接服务器")
        val libraryId = uploadLibrary.value
        val directory = uploadDirectory.value
        val mode = UploadMode.valueOf(uploadMode.value)
        val snapshot = queue.value ?: error("请先恢复传输记录")
        val stems = editedStems.value.toMap()
        val draftFiles = snapshot.drafts.filter { it.intakeGroup == null }.map { draft ->
            val stem = stems[draft.id]
            require(stem == null || draft.validStem(stem)) { "请输入有效的作品名" }
            draft.withEditedStem(stem)
        }
        val inputs = snapshot.pendingInputs.filter { it.intakeGroup == null }.map { it.id }
        check(draftFiles.all { it.id in inputs }) { "旧待传项缺少原始输入身份，请重新选择" }
        check(libraries.value.any { it.id == libraryId && it.enabled }) { "请选择正在接收上传的影库" }
        val family = uploadFamily.value.trim().takeIf { mode == UploadMode.ORDINARY && (uploadAutoName.value || uploadCollectionId.value != null) }?.takeIf(String::isNotBlank)
        val options = UploadOptions(uploadAuthorIds.value, uploadCollectionId.value, uploadNote.value, uploadSources.value.lines().filter(String::isNotBlank), family,
            if (uploadAuthorIds.value.isEmpty()) emptyList() else uploadAuthorSources.value.lines().map(String::trim).filter(String::isNotEmpty))
        val namingDrafts = draftFiles.filter { it.bytes > 0 }
        if (mode == UploadMode.ORDINARY && (uploadAutoName.value || options.collectionId != null) && namingDrafts.isNotEmpty()) {
            val inspection = graph.naming.inspect(NamingRequest(libraryId, directory, family.orEmpty(), options.collectionId,
                if (namingDrafts.all { it.mimeType.startsWith("video/") }) "video" else "image",
                options.note, namingDrafts.map { NamingFile(it.id, it.name) }))
            check(inspection.canSubmit) { "请先填写作品基名并选择文件" }
        }
        check(sameTarget(target, session.value)) { "服务器已改变，请重新确认上传位置" }
        graph.uploads.enqueue(libraryId, directory, mode, options, target, inputs, reviewed = draftFiles, editedStems = stems)
        saved["uploadStems"] = HashMap(editedStems.value.filterKeys { it !in inputs })
    }
    fun editStem(draft: UploadDraft, stem: String) {
        saved["uploadStems"] = HashMap(editedStems.value).apply { put(draft.id, stem) }
        nameWrites.remove(draft.id)?.cancel()
        if (!draft.validStem(stem)) return
        nameWrites[draft.id] = viewModelScope.launch {
            try { delay(350); graph.uploads.renameDraft(draft.id, draft.withEditedStem(stem).name) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutableMessage.value = explain(failure) }
            finally { if (nameWrites[draft.id] == coroutineContext[Job]) nameWrites.remove(draft.id) }
        }
    }
    fun removeDraft(id: String) = action {
        nameWrites.remove(id)?.cancel()
        graph.uploads.discardDraft(id)
        saved["uploadStems"] = HashMap(editedStems.value).apply { remove(id) }
    }
    fun retryUpload(id: String) = action { graph.uploads.retry(id) }
    fun cancelUpload(id: String) = action { graph.uploads.cancel(id) }
    fun clearUploads() = action {
        val previously = queue.value?.transfers.orEmpty().filter { it.archiveSourceState != null }.mapTo(mutableSetOf(), Transfer::id)
        graph.uploads.clearFinished()
        graph.archives.prune(previously - queue.value?.transfers.orEmpty().map(Transfer::id).toSet())
    }
    fun retryArchiveSource(task: Transfer) = action {
        graph.archives.retryProtection(task)
        graph.queue.update { data -> data.copy(transfers = data.transfers.map {
            if (it.id == task.id) it.copy(archiveSourceState = "pending", message = "已入库；正在重新核对本批独立备份") else it
        }) }
        graph.uploads.schedule()
    }
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
                if (generation == connectionGeneration) state.value = ListState(if (next) old.items + result.items else result.items, result.nextCursor,
                    loaded = true, totalMatched = result.totalMatched, dateCounts = if (next) old.dateCounts else result.dateCounts)
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
