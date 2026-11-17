package media.qimeng.next

import android.app.Application
import android.net.Uri
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.model.*

class AuthorProfileViewModel(application: Application) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    private val repository = graph.authorProfiles
    private val mutableProfile = MutableStateFlow(LoadState<ProfileDocument>())
    val profile = mutableProfile.asStateFlow()
    private val mutableCandidates = MutableStateFlow(ListState<ProfileCandidate>())
    val candidates = mutableCandidates.asStateFlow()
    private val mutableArchives = MutableStateFlow(ListState<ProfileArchive>())
    val archives = mutableArchives.asStateFlow()
    private val mutableBusy = MutableStateFlow(false)
    val busy = mutableBusy.asStateFlow()
    private val mutableError = MutableStateFlow<String?>(null)
    val error = mutableError.asStateFlow()
    private val summaries = linkedMapOf<String, MutableStateFlow<LoadState<ProfileSummary>>>()
    private val summaryJobs = mutableMapOf<String, Job>()
    private var profileJob: Job? = null
    private var candidateJob: Job? = null
    private var archiveJob: Job? = null
    private var writeJob: Job? = null
    private var activeAuthor: String? = null
    private var activeArchive: String? = null
    private var connection = 0L
    private var pendingAvatar: Pair<ProfileDocument, Long>? = null
    init {
        viewModelScope.launch { graph.settings.session.collect {
            connection++
            profileJob?.cancel(); candidateJob?.cancel(); archiveJob?.cancel(); writeJob?.cancel()
            summaryJobs.values.forEach(Job::cancel); summaryJobs.clear()
            summaries.values.forEach { it.value = LoadState() }; summaries.clear()
            mutableProfile.value = LoadState(); mutableCandidates.value = ListState(); mutableArchives.value = ListState()
            mutableBusy.value = false; mutableError.value = null; activeAuthor = null; activeArchive = null; pendingAvatar = null
        } }
    }
    fun summary(id: String): StateFlow<LoadState<ProfileSummary>> = summaryState(id)
    private fun summaryState(id: String): MutableStateFlow<LoadState<ProfileSummary>> {
        summaries[id]?.let { return it }
        if (summaries.size >= 100) {
            val expired = summaries.keys.first()
            summaries.remove(expired); summaryJobs.remove(expired)?.cancel()
        }
        return MutableStateFlow(LoadState<ProfileSummary>()).also { summaries[id] = it }
    }
    fun loadSummary(id: String, force: Boolean = false) {
        val state = summaryState(id)
        if (!force && (state.value.data != null || state.value.loading)) return
        summaryJobs.remove(id)?.cancel()
        state.value = state.value.copy(loading = true, error = null)
        val generation = connection
        summaryJobs[id] = viewModelScope.launch {
            try { val value = repository.summary(id); if (generation == connection) state.value = LoadState(data = value) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { if (generation == connection) state.value = LoadState(error = "头像与摘要尚未读取") }
        }
    }
    fun load(id: String, archiveId: String? = null) {
        profileJob?.cancel(); candidateJob?.cancel(); archiveJob?.cancel()
        val same = activeAuthor == id && activeArchive == archiveId
        activeAuthor = id; activeArchive = archiveId
        mutableProfile.value = if (same) mutableProfile.value.copy(loading = true, error = null) else LoadState(loading = true)
        if (!same) { mutableCandidates.value = ListState(); mutableArchives.value = ListState() }
        mutableError.value = null
        val generation = connection
        profileJob = viewModelScope.launch {
            try {
                val value = if (archiveId == null) repository.profile(id) else repository.archive(id, archiveId)
                if (isCurrent(id, archiveId, generation)) mutableProfile.value = LoadState(data = value)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (isCurrent(id, archiveId, generation)) mutableProfile.value = mutableProfile.value.copy(loading = false, error = explainProfile(failure)) }
        }
    }
    fun loadCandidates(next: Boolean = false) {
        val id = activeAuthor ?: return
        val archive = activeArchive
        val old = mutableCandidates.value.copy(loading = false)
        if (next && (mutableCandidates.value.loading || old.cursor == null)) return
        candidateJob?.cancel(); mutableCandidates.value = old.copy(loading = true, error = null)
        val generation = connection
        candidateJob = viewModelScope.launch {
            try { val page = repository.candidates(id, if (next) old.cursor else null, archive)
                if (isCurrent(id, archive, generation)) mutableCandidates.value = ListState(if (next) old.items + page.items else page.items, page.nextCursor, loaded = true)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (isCurrent(id, archive, generation)) mutableCandidates.value = old.copy(error = explainProfile(failure)) }
        }
    }
    fun loadArchives(next: Boolean = false) {
        val id = activeAuthor ?: return
        val old = mutableArchives.value.copy(loading = false)
        if (next && (mutableArchives.value.loading || old.cursor == null)) return
        archiveJob?.cancel(); mutableArchives.value = old.copy(loading = true, error = null)
        val generation = connection
        archiveJob = viewModelScope.launch {
            try { val page = repository.archives(id, if (next) old.cursor else null)
                if (generation == connection && activeAuthor == id) mutableArchives.value = ListState(if (next) old.items + page.items else page.items, page.nextCursor, loaded = true)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (generation == connection && activeAuthor == id) mutableArchives.value = old.copy(error = explainProfile(failure)) }
        }
    }
    fun save(document: ProfileDocument, block: ProfileBlock, values: ProfileValues, evidence: List<ProfileEvidence>, candidateId: String?, reviewed: Boolean, onSaved: () -> Unit) = mutate(document) {
        publish(document, repository.save(document, block, values, evidence, candidateId, reviewed))
        onSaved(); if (candidateId != null) loadCandidates()
    }
    fun dismiss(document: ProfileDocument, candidate: ProfileCandidate) = mutate(document) { repository.dismiss(document, candidate); loadCandidates() }
    fun fetchAvatar(document: ProfileDocument, block: ProfileBlock, url: String, onSaved: () -> Unit) = mutate(document) {
        publish(document, repository.fetchAvatar(document, block.revision, url)); onSaved()
    }
    fun prepareAvatar(document: ProfileDocument, revision: Long) { pendingAvatar = document to revision }
    fun uploadAvatar(uri: Uri?) {
        val pending = pendingAvatar
        pendingAvatar = null
        if (uri == null) return
        if (pending == null) { mutableError.value = "头像选择已中断，请重新选择"; return }
        mutate(pending.first) { publish(pending.first, repository.uploadAvatar(pending.first, pending.second, uri)) }
    }
    fun clearAvatar(document: ProfileDocument, revision: Long) = mutate(document) { publish(document, repository.clearAvatar(document, revision)) }
    private fun publish(document: ProfileDocument, block: ProfileBlock) {
        if (activeAuthor == document.authorId && activeArchive == null) mutableProfile.value.data?.let { current ->
            mutableProfile.value = LoadState(data = current.copy(blocks = current.blocks.map { if (it.block == block.block) block else it }))
        }
        loadSummary(document.authorId, force = true)
    }
    private fun mutate(document: ProfileDocument, action: suspend () -> Unit) {
        if (mutableBusy.value) return
        check(document.editable) { "此档案只读" }
        profileJob?.cancel(); mutableBusy.value = true; mutableError.value = null
        val generation = connection
        writeJob = viewModelScope.launch {
            try { action() }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (generation == connection && activeAuthor == document.authorId) mutableError.value = explainProfile(failure) }
            finally { if (generation == connection) mutableBusy.value = false }
        }
    }
    private fun isCurrent(id: String, archive: String?, generation: Long) = activeAuthor == id && activeArchive == archive && connection == generation
}

private fun explainProfile(failure: Exception) = when (failure) {
    is ApiFailure -> failure.message
    is IllegalArgumentException, is IllegalStateException -> failure.message ?: "资料操作未完成"
    else -> "资料读取或保存失败，请重试"
}
