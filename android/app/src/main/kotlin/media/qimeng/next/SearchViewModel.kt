package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import media.qimeng.next.model.Session
import media.qimeng.next.model.SearchWord
import media.qimeng.next.model.sameTarget

class SearchViewModel(application: Application, private val saved: SavedStateHandle) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    val query = saved.getStateFlow("query", "")
    val submitted = saved.getStateFlow("submitted", "")
    val phase = saved.getStateFlow("phase", "entry")
    val history = graph.settings.searchHistory
    // 入口推荐词与输入候选分开持有（旧版 SearchUiState.recommendWords/suggestions 同构）：清词立即回到
    // 上次推荐词；候选在新请求在途时保留上一批，输入过程不闪空，也没有任何加载进度态。
    private val mutableRecommendWords = MutableStateFlow(LoadState<List<SearchWord>>())
    val recommendWords = mutableRecommendWords.asStateFlow()
    private val mutableWords = MutableStateFlow(LoadState<List<SearchWord>>())
    val words = mutableWords.asStateFlow()
    private val mutableError = MutableStateFlow<String?>(null)
    val error = mutableError.asStateFlow()
    private var lookup: Job? = null
    private var recommendLookup: Job? = null
    init {
        synchronizeTarget(graph.settings.session.value)
        viewModelScope.launch {
            graph.settings.session.collect { target ->
                synchronizeTarget(target)
                if (phase.value == "result") return@collect
                if (phase.value == "suggest") suggest() else loadRecommendWords()
            }
        }
    }

    private fun synchronizeTarget(target: Session?) {
        if (saved.get<String>("searchInstance") == target?.instanceId &&
            saved.get<String>("searchEndpoint") == target?.baseUrl) return
        clearQuery()
        mutableRecommendWords.value = LoadState() // 换服务器后不再展示上一实例的推荐词
        saved["openedTag"] = null
        saved["searchInstance"] = target?.instanceId
        saved["searchEndpoint"] = target?.baseUrl
    }

    /** 进页：携词跳转按词提交；否则只在尚未取到列表时补拉（旧版推荐词/候选只随 init 与返回入口加载）。 */
    fun open(initialQuery: String?) {
        synchronizeTarget(graph.settings.session.value)
        if (!initialQuery.isNullOrBlank() && saved.get<String>("openedTag") != initialQuery) {
            saved["openedTag"] = initialQuery
            saved["initialTag"] = initialQuery.trim()
            submit(initialQuery)
        } else if (phase.value == "suggest") {
            if (mutableWords.value.data == null && lookup?.isActive != true) suggest()
        } else if (phase.value == "entry" && mutableRecommendWords.value.data == null && recommendLookup?.isActive != true) {
            loadRecommendWords()
        }
    }

    /** 输入：空词回入口并清候选；非空立即切建议态，防抖后出网（旧版 onQueryChange）。 */
    fun edit(value: String) {
        saved["query"] = value
        if (value.isBlank()) {
            saved["phase"] = "entry"
            lookup?.cancel()
            mutableWords.value = LoadState()
            // 清词回到入口时，上一次推荐词请求可能已被取消（取消不是失败，不会留下 error），
            // 此时入口页既没有胶囊也没有重试入口。没有数据且没有在途请求就重新补拉一次；
            // 加载中仍不显示进度（用户已明确要求去掉加载条，旧版加载期间也什么都不画）。
            if (mutableRecommendWords.value.data == null && recommendLookup?.isActive != true) loadRecommendWords()
            return
        }
        saved["phase"] = "suggest"
        suggest()
    }

    /** 结果态点搜索栏：按当前词回建议态重拉（旧版 backToSuggest → onQueryChange 同链）。 */
    fun enterEditing() {
        if (query.value.isBlank()) {
            saved["phase"] = "entry"
            return
        }
        saved["phase"] = "suggest"
        suggest()
    }

    fun submit(word: String = query.value) {
        val text = word.trim()
        if (text.isEmpty()) return
        lookup?.cancel(); mutableError.value = null
        mutableWords.value = LoadState() // 结果态只展示网格，候选清空；点回搜索栏时按词重拉（旧版 submit 同语义）
        saved["query"] = text; saved["submitted"] = text; saved["phase"] = "result"
        viewModelScope.launch {
            try { withContext(Dispatchers.IO) { graph.settings.recordSearch(text) } }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { mutableError.value = "搜索已提交，但历史未能保存" }
        }
    }
    fun back(): Boolean {
        val returnToStatistics = phase.value == "result" && query.value == saved.get<String>("initialTag")
        val handled = phase.value != "entry" && !returnToStatistics
        clearQuery()
        if (handled) loadRecommendWords() else saved["openedTag"] = null
        return handled
    }

    private fun clearQuery() {
        lookup?.cancel(); recommendLookup?.cancel()
        mutableError.value = null
        mutableWords.value = LoadState()
        saved["initialTag"] = null
        saved["query"] = ""
        saved["submitted"] = ""
        saved["phase"] = "entry"
    }

    fun refreshWords() { if (query.value.isBlank()) loadRecommendWords() else suggest() }

    /**
     * 候选建议（旧版 onQueryChange 算法）：取消上一条 → SUGGEST_DEBOUNCE_MS 防抖 → 按词出网；
     * 响应只在词条未变且不在结果态时回填，被取代的旧响应不会覆盖新输入。
     */
    private fun suggest() {
        lookup?.cancel()
        val target = graph.settings.session.value ?: return
        val text = query.value.trim()
        if (text.isEmpty() || phase.value == "result") return
        if (mutableWords.value.error != null) mutableWords.value = mutableWords.value.copy(error = null)
        lookup = viewModelScope.launch {
            delay(SUGGEST_DEBOUNCE_MS)
            try {
                val words = graph.media.searchSuggestions(text, recommend = false, target = target)
                if (sameTarget(target, graph.settings.session.value) && query.value.trim() == text && phase.value != "result")
                    mutableWords.value = LoadState(data = words)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) {
                if (sameTarget(target, graph.settings.session.value) && query.value.trim() == text && phase.value != "result")
                    mutableWords.value = mutableWords.value.copy(error = "无法读取搜索词，请重试")
            }
        }
    }

    /** 入口推荐词（旧版 loadRecommendWords：q 空 + recommend=true）；加载中保留上一批，不显示进度态。 */
    private fun loadRecommendWords() {
        recommendLookup?.cancel()
        val target = graph.settings.session.value ?: return
        if (mutableRecommendWords.value.error != null) mutableRecommendWords.value = mutableRecommendWords.value.copy(error = null)
        recommendLookup = viewModelScope.launch {
            try {
                val words = graph.media.searchSuggestions("", recommend = true, target = target)
                if (sameTarget(target, graph.settings.session.value)) mutableRecommendWords.value = LoadState(data = words)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) {
                if (sameTarget(target, graph.settings.session.value))
                    mutableRecommendWords.value = mutableRecommendWords.value.copy(error = "无法读取搜索词，请重试")
            }
        }
    }

    fun clearHistory() { viewModelScope.launch {
        try { withContext(Dispatchers.IO) { graph.settings.clearSearchHistory() }; mutableError.value = null }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (_: Exception) { mutableError.value = "清除搜索历史失败，请重试" }
    } }

    private companion object {
        /** 输入防抖 300ms（沿用旧版 feature/search SearchViewModel.SUGGEST_DEBOUNCE_MS 原值） */
        const val SUGGEST_DEBOUNCE_MS = 300L
    }
}
