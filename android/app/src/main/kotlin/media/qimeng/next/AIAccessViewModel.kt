package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.model.*

class AIAccessViewModel(application: Application) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    private val repository = graph.aiAccess
    private val mutableState = MutableStateFlow(ListState<AIAccessGrant>())
    val state = mutableState.asStateFlow()
    private val mutableFilter = MutableStateFlow("active")
    val filter = mutableFilter.asStateFlow()
    private val mutableBusy = MutableStateFlow(false)
    val busy = mutableBusy.asStateFlow()
    // Deliberately not a SavedStateHandle, preference, journal, or replayable event log.
    private val mutableSecret = MutableStateFlow<String?>(null)
    val secret = mutableSecret.asStateFlow()
    private val mutableCreationError = MutableStateFlow<String?>(null)
    val creationError = mutableCreationError.asStateFlow()
    private val mutableReconcileRequired = MutableStateFlow(false)
    val reconcileRequired = mutableReconcileRequired.asStateFlow()
    private var work: Job? = null
    private var read: Job? = null
    private var connection = 0L
    init {
        viewModelScope.launch { graph.settings.session.collect {
            connection++
            work?.cancel(); read?.cancel(); mutableSecret.value = null; mutableState.value = ListState(); mutableBusy.value = false; mutableReconcileRequired.value = false; mutableFilter.value = "active"
        } }
    }
    fun refresh(next: Boolean = false, state: String = mutableFilter.value) {
        if (mutableBusy.value) return
        val same = state == mutableFilter.value
        val old = if (same) mutableState.value.copy(loading = false) else ListState<AIAccessGrant>()
        if (next && (mutableState.value.loading || old.cursor == null)) return
        read?.cancel(); mutableFilter.value = state
        mutableState.value = old.copy(loading = true, error = null)
        read = viewModelScope.launch {
            try {
                val page = repository.list(state, if (next) old.cursor else null)
                mutableState.value = ListState(if (next) old.items + page.items else page.items, page.nextCursor, loaded = true)
                if (!next && state == "active") mutableReconcileRequired.value = false
            }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { mutableState.value = old.copy(error = "无法读取 AI 接入列表，请重试") }
        }
    }
    fun create(policy: AIAccessPolicy, onCreated: () -> Unit) {
        if (mutableBusy.value) return
        work?.cancel(); read?.cancel(); mutableBusy.value = true; mutableCreationError.value = null
        val generation = connection
        work = viewModelScope.launch {
            var refreshAfter = false
            try {
                val (_, token) = repository.create(policy)
                if (generation != connection) return@launch
                mutableSecret.value = token
                refreshAfter = true
                onCreated()
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: ApiFailure) { mutableCreationError.value = failure.message }
            catch (_: Exception) {
                mutableReconcileRequired.value = true
                mutableCreationError.value = "创建结果未获确认。请关闭表单并刷新连接列表核对，勿重复创建；无可用凭据的连接可先撤销。"
            }
            finally { if (generation == connection) { mutableBusy.value = false; mutableState.value = mutableState.value.copy(loading = false); if (refreshAfter) refresh(state = "active") } }
        }
    }
    fun revoke(id: String) {
        if (mutableBusy.value) return
        work?.cancel(); read?.cancel(); mutableBusy.value = true
        val generation = connection
        work = viewModelScope.launch {
            var refreshAfter = false
            try {
                repository.revoke(id)
                if (generation != connection) return@launch
                refreshAfter = true
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { mutableState.value = mutableState.value.copy(error = "撤销未获确认，请刷新列表核对") }
            finally { if (generation == connection) { mutableBusy.value = false; mutableState.value = mutableState.value.copy(loading = false); if (refreshAfter) refresh() } }
        }
    }
    fun dismissSecret() { mutableSecret.value = null }
    fun clearCreationError() { if (!mutableReconcileRequired.value) mutableCreationError.value = null }
    override fun onCleared() { mutableSecret.value = null; super.onCleared() }
}
