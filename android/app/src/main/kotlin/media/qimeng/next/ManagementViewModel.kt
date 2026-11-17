package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.model.*

class ManagementViewModel(application: Application) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    private val repository = graph.management
    private val mutableEntries = MutableStateFlow(ListState<VocabularyEntry>())
    val entries = mutableEntries.asStateFlow()
    private val mutableSources = MutableStateFlow(ListState<VocabularyEntry>())
    val sources = mutableSources.asStateFlow()
    private val mutableMarks = MutableStateFlow(ListState<TimelineMark>())
    val marks = mutableMarks.asStateFlow()
    private val mutableBusy = MutableStateFlow(false)
    val busy = mutableBusy.asStateFlow()
    private val mutableError = MutableStateFlow<String?>(null)
    val error = mutableError.asStateFlow()
    private var listJob: Job? = null
    private var sourceJob: Job? = null
    private var markJob: Job? = null
    private var mutation: Job? = null
    private var entryKind: String? = null
    private var markScope: String? = null
    init {
        viewModelScope.launch { graph.settings.session.collect {
            listJob?.cancel(); sourceJob?.cancel(); markJob?.cancel(); mutation?.cancel()
            mutableEntries.value = ListState(); mutableSources.value = ListState(); mutableMarks.value = ListState()
            entryKind = null; markScope = null
            mutableBusy.value = false; mutableError.value = null
        } }
    }
    fun load(kind: String, next: Boolean = false) {
        if (next && (mutableEntries.value.loading || mutableEntries.value.cursor == null)) return
        listJob?.cancel()
        val previous: ListState<VocabularyEntry> = if (entryKind == kind) mutableEntries.value else ListState()
        entryKind = kind
        mutableEntries.value = if (next) previous.copy(loading = true, error = null) else ListState(loading = true)
        listJob = viewModelScope.launch {
            try { val page = repository.vocabulary(kind, if (next) previous.cursor else null)
                mutableEntries.value = ListState(if (next) previous.items + page.items else page.items, page.nextCursor, loaded = true)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutableEntries.value = previous.copy(loading = false, error = explanation(failure)) }
        }
    }
    fun loadSources(next: Boolean = false) {
        if (mutableSources.value.loading || (next && mutableSources.value.cursor == null)) return
        val previous = mutableSources.value
        mutableSources.value = previous.copy(loading = true)
        sourceJob = viewModelScope.launch {
            try { val page = repository.vocabulary("sources", if (next) previous.cursor else null)
                mutableSources.value = ListState(if (next) previous.items + page.items else page.items, page.nextCursor, loaded = true)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutableSources.value = previous.copy(error = explanation(failure)) }
        }
    }
    fun save(kind: String, item: VocabularyEntry?, name: String, aliases: List<String>, source: String?, onSaved: () -> Unit) = mutate {
        repository.saveVocabulary(kind, item, name, aliases, source); onSaved(); load(kind)
    }
    fun deleteTag(item: VocabularyEntry) = mutate { repository.deleteTag(item); load("tags") }
    fun loadMarks(asset: Asset, next: Boolean = false) {
        if (next && (mutableMarks.value.loading || mutableMarks.value.cursor == null)) return
        markJob?.cancel()
        val scope = "${asset.target.instanceId}:${asset.id}"
        val previous: ListState<TimelineMark> = if (markScope == scope) mutableMarks.value else ListState()
        markScope = scope
        mutableMarks.value = if (next) previous.copy(loading = true, error = null) else ListState(loading = true)
        markJob = viewModelScope.launch {
            try { val page = repository.marks(asset, if (next) previous.cursor else null)
                mutableMarks.value = ListState(if (next) previous.items + page.items else page.items, page.nextCursor, loaded = true)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutableMarks.value = previous.copy(error = explanation(failure)) }
        }
    }
    fun saveMark(asset: Asset, previous: TimelineMark?, position: Long, label: String, color: String, saved: () -> Unit) = mutate {
        repository.saveMark(asset, previous, position, label, color); saved(); loadMarks(asset)
    }
    fun deleteMark(asset: Asset, mark: TimelineMark) = mutate { repository.deleteMark(asset, mark); loadMarks(asset) }
    private fun mutate(operation: suspend () -> Unit) {
        if (mutableBusy.value) return
        mutableBusy.value = true; mutableError.value = null
        mutation = viewModelScope.launch {
            try { operation() }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutableError.value = explanation(failure) }
            finally { mutableBusy.value = false }
        }
    }
}

private fun explanation(failure: Exception) = when (failure) {
    is ApiFailure -> failure.message
    is java.io.IOException -> "连接失败，请重试"
    is IllegalArgumentException, is IllegalStateException -> failure.message ?: "操作未完成"
    else -> "操作未完成，请重试"
}
