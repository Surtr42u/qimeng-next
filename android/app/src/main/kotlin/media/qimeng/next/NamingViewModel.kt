package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.model.*

data class NamingState(val request: NamingRequest? = null, val inspection: NamingInspection? = null,
    val loading: Boolean = false, val error: String? = null)

class NamingViewModel(application: Application) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    private val mutableState = MutableStateFlow(NamingState())
    val state = mutableState.asStateFlow()
    private var work: Job? = null
    init { viewModelScope.launch { graph.settings.session.collect { work?.cancel(); mutableState.value = NamingState() } } }

    fun inspect(request: NamingRequest?) {
        work?.cancel()
        mutableState.value = NamingState(request = request, loading = request != null)
        if (request == null) return
        work = viewModelScope.launch {
            try {
                delay(350)
                val result = graph.naming.inspect(request)
                mutableState.value = NamingState(request = request, inspection = result)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutableState.value = NamingState(request = request,
                error = if (failure is ApiFailure) failure.message else "无法读取命名预览，请重试") }
        }
    }
}
