package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.model.Asset
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.model.Author
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget
import java.io.IOException

internal sealed interface AlbumBatchChange {
    data class ReplaceAuthor(val author: Author) : AlbumBatchChange
    data class AppendAcquisition(val sources: List<String>) : AlbumBatchChange
}

internal data class AlbumBatchResult(
    val asset: Asset,
    val error: String? = null,
    val conflict: Boolean = false,
    val reviewed: Boolean = false,
)

internal data class AlbumBatchProgress(val completed: Int, val total: Int, val reviewing: Boolean = false)

internal data class AlbumSelectionState(
    val selecting: Boolean = false,
    val selected: Map<String, Asset> = emptyMap(),
    val sheetOpen: Boolean = false,
    val progress: AlbumBatchProgress? = null,
    val report: List<AlbumBatchResult> = emptyList(),
)

internal class AlbumBatchViewModel(application: Application) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    private val repository = graph.media
    private val mutableSelection = MutableStateFlow(AlbumSelectionState())
    val selection = mutableSelection.asStateFlow()
    private val mutableAuthors = MutableStateFlow(LoadState<List<Author>>())
    val authors = mutableAuthors.asStateFlow()
    private var target = repository.session.value
    private var query: AssetQuery? = null
    private var write: Job? = null
    private var lookup: Job? = null
    private var change: AlbumBatchChange? = null

    init {
        viewModelScope.launch { repository.session.collect { synchronizeTarget() } }
    }

    private fun synchronizeTarget(): Session? {
        val current = repository.session.value
        val previous = target
        if (previous != current && (previous == null || !sameTarget(previous, current))) {
            write?.cancel()
            reset()
        }
        target = current
        return current
    }

    fun bindQuery(value: AssetQuery) {
        if (query != null && query != value && mutableSelection.value.progress == null) reset()
        query = value
    }

    fun select(asset: Asset) {
        if (!sameTarget(asset.target, synchronizeTarget())) return
        val state = mutableSelection.value
        val items = if (asset.id in state.selected) state.selected - asset.id else state.selected + (asset.id to asset)
        mutableSelection.value = state.copy(selecting = true, selected = items)
    }

    fun selectLoaded(assets: List<Asset>) {
        val current = synchronizeTarget() ?: return
        val state = mutableSelection.value
        val items = assets.filter { sameTarget(it.target, current) }.associate { it.id to (state.selected[it.id] ?: it) }
        mutableSelection.value = state.copy(selected = items)
    }

    fun clear() { mutableSelection.value = mutableSelection.value.copy(selected = emptyMap()) }

    fun exit() {
        if (mutableSelection.value.progress == null) reset()
    }

    private fun reset() {
        lookup?.cancel()
        change = null
        mutableAuthors.value = LoadState()
        mutableSelection.value = AlbumSelectionState()
    }

    fun openSheet() {
        synchronizeTarget()
        val state = mutableSelection.value
        if (state.selected.isNotEmpty()) mutableSelection.value = state.copy(sheetOpen = true)
    }

    fun closeSheet() {
        val state = mutableSelection.value
        if (state.progress != null) return
        lookup?.cancel()
        mutableAuthors.value = LoadState()
        change = null
        mutableSelection.value = state.copy(sheetOpen = false, report = emptyList())
    }

    fun findAuthors(text: String) {
        val current = synchronizeTarget() ?: return
        lookup?.cancel()
        mutableAuthors.value = LoadState(loading = true)
        lookup = viewModelScope.launch {
            try {
                if (text.isNotBlank()) delay(220)
                val page = repository.authors(query = text.trim().ifEmpty { null }, kind = "regular", limit = 8, target = current)
                if (sameTarget(current, repository.session.value)) {
                    mutableAuthors.value = LoadState(data = page.items.filterNot { it.id == "unknown" })
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (sameTarget(current, repository.session.value)) mutableAuthors.value = LoadState(error = batchError(failure))
            }
        }
    }

    fun submit(operation: AlbumBatchChange, onUpdated: (Session) -> Unit) {
        val current = synchronizeTarget() ?: return
        val state = mutableSelection.value
        if (state.selected.isEmpty() || state.progress != null || state.report.isNotEmpty()) return
        lookup?.cancel()
        change = operation
        apply(current, operation, state.selected.values.toList(), onUpdated)
    }

    fun retry(onUpdated: (Session) -> Unit) {
        val current = synchronizeTarget() ?: return
        val state = mutableSelection.value
        val operation = change ?: return
        val remaining = state.report.filter { it.error != null && !it.conflict }.map { it.asset }
        if (state.progress == null && remaining.isNotEmpty()) apply(current, operation, remaining, onUpdated)
    }

    private fun apply(current: Session, operation: AlbumBatchChange, items: List<Asset>, onUpdated: (Session) -> Unit) {
        val progress = AlbumBatchProgress(0, items.size)
        mutableSelection.value = mutableSelection.value.copy(progress = progress)
        write = viewModelScope.launch {
            val results = mutableListOf<AlbumBatchResult>()
            for ((index, asset) in items.withIndex()) {
                if (!sameTarget(current, repository.session.value)) return@launch
                val result = try {
                    val updated = when (operation) {
                        is AlbumBatchChange.ReplaceAuthor -> repository.updateAuthors(
                            asset, listOf(operation.author.id), emptyList(), asset.revision, asset.authorsRevision,
                        )
                        is AlbumBatchChange.AppendAcquisition -> repository.updateAcquisitionSources(
                            asset, (asset.acquisitionSources + operation.sources).distinct(),
                        )
                    }
                    AlbumBatchResult(updated)
                } catch (cancelled: CancellationException) { throw cancelled }
                catch (failure: Exception) {
                    AlbumBatchResult(asset, batchError(failure), conflict = failure is ApiFailure && failure.code == "revision_conflict")
                }
                if (!sameTarget(current, repository.session.value)) return@launch
                results += result
                mutableSelection.value = mutableSelection.value.copy(progress = progress.copy(completed = index + 1))
            }
            val report = mergeReport(results)
            val failures = report.filter { it.error != null }.associate { it.asset.id to it.asset }
            mutableSelection.value = mutableSelection.value.copy(
                selecting = failures.isNotEmpty(), selected = failures, progress = null, report = report,
            )
            if (results.any { it.error == null }) onUpdated(current)
        }
    }

    fun reviewConflicts() {
        val current = synchronizeTarget() ?: return
        val state = mutableSelection.value
        val conflicts = state.report.filter(AlbumBatchResult::conflict)
        if (state.progress != null || conflicts.isEmpty()) return
        val progress = AlbumBatchProgress(0, conflicts.size, reviewing = true)
        mutableSelection.value = state.copy(progress = progress)
        write = viewModelScope.launch {
            val results = mutableListOf<AlbumBatchResult>()
            for ((index, result) in conflicts.withIndex()) {
                if (!sameTarget(current, repository.session.value)) return@launch
                val reviewed = try {
                    val latest = repository.asset(result.asset.id, current)
                    check(latest.id == result.asset.id) { "媒体身份已变更，请重新选择" }
                    AlbumBatchResult(latest, "已读取最新状态，请确认重试", reviewed = true)
                } catch (cancelled: CancellationException) { throw cancelled }
                catch (failure: Exception) {
                    result.copy(error = batchError(failure))
                }
                if (!sameTarget(current, repository.session.value)) return@launch
                results += reviewed
                mutableSelection.value = mutableSelection.value.copy(progress = progress.copy(completed = index + 1))
            }
            val report = mergeReport(results)
            mutableSelection.value = mutableSelection.value.copy(
                selected = report.filter { it.error != null }.associate { it.asset.id to it.asset },
                progress = null, report = report,
            )
        }
    }

    private fun mergeReport(updates: List<AlbumBatchResult>): List<AlbumBatchResult> {
        val previous = mutableSelection.value.report
        if (previous.isEmpty()) return updates
        val byId = updates.associateBy { it.asset.id }
        return previous.map { byId[it.asset.id] ?: it }
    }

    fun reviewedDetails(asset: Asset): String = when (change) {
        is AlbumBatchChange.ReplaceAuthor -> {
            val current = asset.authorName?.let { "首位作者 $it" } ?: "暂无作者"
            "$current，保存后替换常规作者"
        }
        is AlbumBatchChange.AppendAcquisition -> {
            val current = asset.acquisitionSources.joinToString("、").ifEmpty { "无" }
            "现有获取来源 $current"
        }
        null -> ""
    }
}

private fun batchError(failure: Exception): String = when (failure) {
    is ApiFailure -> failure.message
    is IOException -> "连接失败，请检查服务器后重试"
    is IllegalStateException -> failure.message ?: "状态已变化，请重新选择"
    else -> "操作未完成，请重试"
}
