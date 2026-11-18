package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch
import media.qimeng.next.model.DevicePackage
import media.qimeng.next.model.Session

class DeviceDeliveryViewModel(application: Application) : AndroidViewModel(application) {
    private val app = application as QimengApplication
    private val repo = app.graph.deviceDelivery
    val session = app.graph.settings.session
    private val current = MutableStateFlow(repo.view())
    val settings = current.asStateFlow()
    private val results = MutableStateFlow<List<DevicePackage>?>(null)
    val recent = results.asStateFlow()
    private val working = MutableStateFlow(false)
    val busy = working.asStateFlow()
    private val errorState = MutableStateFlow<String?>(null)
    val error = errorState.asStateFlow()
    private var operation: Job? = null
    init { viewModelScope.launch {
        var previous: Session? = null
        combine(session, repo.revision) { target, _ -> target }.collect { target ->
            if (previous != target) { operation?.cancel(); results.value = null; working.value = false; errorState.value = null; previous = target }
            current.value = repo.view()
        }
    } }
    fun local(target: Session?) = target != null && app.isSelectedLocalInstance(target)
    fun save(address: String, device: String, secret: String, replaceAuthorProfiles: Boolean, applyNow: Boolean, complete: () -> Unit) = act { target ->
        repo.saveSettings(target, address, device, secret, replaceAuthorProfiles)
        if (session.value == target) {
            complete()
            if (applyNow) repo.schedule(target, replaceAuthorProfiles)
            else if (secret.isNotBlank()) {
                val page = repo.recent()
                if (session.value == target) results.value = page
            }
        }
    }
    fun issue(device: String) = act { repo.generate(it, device, device); results.value = repo.recent() }
    fun clear() = act { repo.clear(); results.value = null }
    fun toggle(on: Boolean) = act { repo.background(it, on) }
    fun refresh() = act { target -> val page = repo.recent(); if (session.value == target) results.value = page }
    private fun act(block: suspend (Session) -> Unit) {
        val target = session.value ?: return
        if (working.value) return
        working.value = true; errorState.value = null
        operation = viewModelScope.launch {
            try { block(target) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) { if (session.value == target) errorState.value = libraryError(error) }
            finally { if (session.value == target) working.value = false }
        }
    }
}
