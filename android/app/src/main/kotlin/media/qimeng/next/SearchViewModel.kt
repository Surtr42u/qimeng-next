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
    private val mutableWords = MutableStateFlow(LoadState<List<SearchWord>>())
    val words = mutableWords.asStateFlow()
    private val mutableError = MutableStateFlow<String?>(null)
    val error = mutableError.asStateFlow()
    private var lookup: Job? = null
    init {
        synchronizeTarget(graph.settings.session.value)
        viewModelScope.launch {
            graph.settings.session.collect { target ->
                synchronizeTarget(target)
                refreshWords()
            }
        }
    }

    private fun synchronizeTarget(target: Session?) {
        if (saved.get<String>("searchInstance") == target?.instanceId &&
            saved.get<String>("searchEndpoint") == target?.baseUrl) return
        clearQuery()
        saved["openedTag"] = null
        saved["searchInstance"] = target?.instanceId
        saved["searchEndpoint"] = target?.baseUrl
    }

    fun open(initialQuery: String?) {
        synchronizeTarget(graph.settings.session.value)
        if (!initialQuery.isNullOrBlank() && saved.get<String>("openedTag") != initialQuery) {
            saved["openedTag"] = initialQuery
            saved["initialTag"] = initialQuery.trim()
            submit(initialQuery)
        } else if (phase.value == "entry") refreshWords()
    }

    fun edit(value: String) {
        saved["query"] = value
        saved["phase"] = if (value.isBlank()) "entry" else "suggest"
        refreshWords()
    }
    fun enterEditing() { saved["phase"] = if (query.value.isBlank()) "entry" else "suggest"; refreshWords() }
    fun submit(word: String = query.value) {
        val text = word.trim()
        if (text.isEmpty()) return
        lookup?.cancel(); mutableError.value = null
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
        if (handled) refreshWords() else saved["openedTag"] = null
        return handled
    }

    private fun clearQuery() {
        lookup?.cancel()
        mutableError.value = null
        mutableWords.value = LoadState()
        saved["initialTag"] = null
        saved["query"] = ""
        saved["submitted"] = ""
        saved["phase"] = "entry"
    }

    fun refreshWords() {
        lookup?.cancel()
        val target = graph.settings.session.value ?: return
        if (phase.value == "result") return
        val text = query.value.trim()
        mutableWords.value = LoadState(loading = true)
        lookup = viewModelScope.launch {
            try {
                if (text.isNotEmpty()) delay(250)
                val words = graph.media.searchSuggestions(text, recommend = text.isEmpty(), target = target)
                if (sameTarget(target, graph.settings.session.value)) mutableWords.value = LoadState(data = words)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) {
                if (sameTarget(target, graph.settings.session.value)) mutableWords.value = LoadState(error = "无法读取搜索词，请重试")
            }
        }
    }
    fun clearHistory() { viewModelScope.launch {
        try { withContext(Dispatchers.IO) { graph.settings.clearSearchHistory() }; mutableError.value = null }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (_: Exception) { mutableError.value = "清除搜索历史失败，请重试" }
    } }
}
