package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.model.*

/**
 * [inspection] 与其来源请求 [inspectionRequest] 分开记录：输入变化后的防抖与请求期间继续显示上一份预览，
 * 只有新请求成功才替换。提交门禁仍须 [inspectionRequest] 等于当前请求，旧预览不能当作本次输入的结果。
 */
data class NamingState(val request: NamingRequest? = null, val inspectionRequest: NamingRequest? = null,
    val inspection: NamingInspection? = null, val loading: Boolean = false, val error: String? = null)

class NamingViewModel(application: Application) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    private val mutableState = MutableStateFlow(NamingState())
    val state = mutableState.asStateFlow()
    private var work: Job? = null
    init { viewModelScope.launch { graph.settings.session.collect { work?.cancel(); mutableState.value = NamingState() } } }

    fun inspect(request: NamingRequest?) {
        work?.cancel()
        if (request == null) {
            mutableState.value = NamingState()
            return
        }
        val previous = mutableState.value
        // 重查不清掉上一份预览：防抖与请求期间保留旧内容，只把 request 指向新输入并标记加载中。
        mutableState.value = previous.copy(request = request, loading = true, error = null)
        work = viewModelScope.launch {
            try {
                delay(350)
                val result = graph.naming.inspect(request)
                mutableState.value = NamingState(request = request, inspectionRequest = request, inspection = result)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { mutableState.value = NamingState(request = request,
                inspectionRequest = previous.inspectionRequest, inspection = previous.inspection,
                error = if (failure is ApiFailure) failure.message else "无法读取命名预览，请重试") }
        }
    }
}
