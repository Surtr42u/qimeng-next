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
    private val mutableState = MutableStateFlow(LoadState<List<AIAccessGrant>>())
    val state = mutableState.asStateFlow()
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
    init {
        viewModelScope.launch { graph.settings.session.collect {
            work?.cancel(); mutableSecret.value = null; mutableState.value = LoadState(); mutableBusy.value = false; mutableReconcileRequired.value = false
        } }
    }
    fun refresh() {
        if (mutableBusy.value) return
        work?.cancel()
        mutableState.value = mutableState.value.copy(loading = true, error = null)
        work = viewModelScope.launch {
            try { mutableState.value = LoadState(data = repository.list()); mutableReconcileRequired.value = false }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { mutableState.value = mutableState.value.copy(loading = false, error = "无法读取 AI 接入列表，请重试") }
        }
    }
    fun create(policy: AIAccessPolicy, onCreated: () -> Unit) {
        if (mutableBusy.value) return
        work?.cancel(); mutableBusy.value = true; mutableCreationError.value = null
        work = viewModelScope.launch {
            try {
                val (grant, token) = repository.create(policy)
                mutableState.value = LoadState(data = (mutableState.value.data.orEmpty() + grant).distinctBy { it.id })
                mutableSecret.value = token
                onCreated()
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: ApiFailure) { mutableCreationError.value = failure.message }
            catch (_: Exception) {
                mutableReconcileRequired.value = true
                mutableCreationError.value = "创建结果未获确认。请关闭表单并刷新连接列表核对，勿重复创建；无可用凭据的连接可先撤销。"
            }
            finally { mutableBusy.value = false }
        }
    }
    fun revoke(id: String) {
        if (mutableBusy.value) return
        work?.cancel(); mutableBusy.value = true
        work = viewModelScope.launch {
            try {
                repository.revoke(id)
                mutableState.value = LoadState(data = mutableState.value.data.orEmpty().map { if (it.id == id) it.copy(state = "revoked") else it })
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { mutableState.value = mutableState.value.copy(error = "撤销未获确认，请刷新列表核对") }
            finally { mutableBusy.value = false }
        }
    }
    fun dismissSecret() { mutableSecret.value = null }
    fun clearCreationError() { if (!mutableReconcileRequired.value) mutableCreationError.value = null }
    override fun onCleared() { mutableSecret.value = null; super.onCleared() }
}
