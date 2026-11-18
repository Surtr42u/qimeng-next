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
import media.qimeng.next.model.DeviceReceipt
import media.qimeng.next.model.DeviceSegmentResult
import media.qimeng.next.model.Session
import media.qimeng.next.model.Asset
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.model.Page

data class TargetVideoPage(val query: String, val page: Page<Asset>, val selected: Asset? = null, val libraryName: String? = null)

class DeviceDeliveryViewModel(application: Application) : AndroidViewModel(application) {
    private val app = application as QimengApplication
    private val repo = app.graph.deviceDelivery
    val session = app.graph.settings.session
    private val current = MutableStateFlow(repo.view())
    val settings = current.asStateFlow()
    private val results = MutableStateFlow<List<DevicePackage>?>(null)
    val recent = results.asStateFlow()
    private val detail = MutableStateFlow<DeviceReceipt?>(null)
    val receipt = detail.asStateFlow()
    private val targetPage = MutableStateFlow<TargetVideoPage?>(null)
    val videos = targetPage.asStateFlow()
    private val working = MutableStateFlow(false)
    val busy = working.asStateFlow()
    private val errorState = MutableStateFlow<String?>(null)
    val error = errorState.asStateFlow()
    private var operation: Job? = null
    init { viewModelScope.launch {
        var previous: Session? = null
        combine(session, repo.revision) { target, _ -> target }.collect { target ->
            if (previous != target) { operation?.cancel(); results.value = null; detail.value = null; targetPage.value = null; working.value = false; errorState.value = null; previous = target }
            current.value = repo.view()
        }
    } }
    fun local(target: Session?) = target != null && app.isSelectedLocalInstance(target)
    fun save(address: String, device: String, secret: String, background: Boolean, replaceAuthorProfiles: Boolean, applyNow: Boolean, complete: () -> Unit) = act { target ->
        repo.saveSettings(target, address, device, secret, background, replaceAuthorProfiles)
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
    fun clear() = act { repo.clear(); results.value = null; detail.value = null }
    fun refresh() = act { target -> val page = repo.recent(); if (session.value == target) results.value = page }
    fun showReceipt(result: DeviceSegmentResult) = act { target ->
        val id = requireNotNull(result.receipt)
        if (detail.value?.id == id) detail.value = null else {
            val fetched = repo.receipt(target, result)
            if (session.value == target) detail.value = fetched
        }
    }
    fun searchVideos(query: String, next: Boolean = false) = act { target ->
        check(local(target)) { "请先连接本机影库" }
        val term = query.trim()
        val cursor = if (next) targetPage.value?.takeIf { it.query == term }?.page?.nextCursor ?: return@act else null
        targetPage.value = null
        val page = app.graph.media.assets(AssetQuery(text = term, mediaType = "video"), cursor)
        if (session.value == target) targetPage.value = TargetVideoPage(term, page)
    }

    fun chooseVideo(video: Asset) = act { target ->
        val list = targetPage.value ?: return@act
        check(local(target) && video.target == target) { "所选视频属于原连接，请重新选择" }
        targetPage.value = list.copy(selected = null, libraryName = null)
        val current = app.graph.media.selectableVideo(target, video.id)
        val library = app.graph.media.library(target, current.libraryId)
        if (session.value == target) targetPage.value = list.copy(selected = current, libraryName = library.name)
    }
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
