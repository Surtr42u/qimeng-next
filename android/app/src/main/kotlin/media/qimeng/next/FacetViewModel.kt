package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.model.FacetChoice
import media.qimeng.next.model.Session

data class FacetRowState(
    val items: List<FacetChoice> = emptyList(), val cursor: String? = null,
    val totalOptions: Long? = null, val totalMatched: Long? = null,
    val loading: Boolean = false, val error: String? = null,
)

/** Each row asks Go for its alternatives under the other three rows and all fixed page constraints. */
class FacetViewModel(application: Application) : AndroidViewModel(application) {
    private val media = (application as QimengApplication).graph.media
    private val mutableRows = MutableStateFlow(emptyMap<String, FacetRowState>())
    val rows = mutableRows.asStateFlow()
    private val requests = mutableMapOf<String, Pair<Session, AssetQuery>>()
    private val jobs = mutableMapOf<String, Job>()

    fun bind(target: Session, query: AssetQuery) {
        val predicates = mapOf("partitions" to query.copy(kind = null), "works" to query.copy(workKeys = emptyList()),
            "roles" to query.copy(roleKeys = emptyList()), "types" to query.copy(mediaType = null))
        for ((row, predicate) in predicates) {
            val request = target to predicate
            if (requests[row] == request) continue
            requests[row] = request
            fetch(row, next = false)
        }
    }

    fun fetch(row: String, next: Boolean) {
        val request = requests[row] ?: return
        val previous = if (next) mutableRows.value[row] ?: return else FacetRowState()
        if (next && (previous.loading || previous.cursor == null)) return
        jobs.remove(row)?.cancel()
        mutableRows.update { it + (row to previous.copy(loading = true, error = null)) }
        jobs[row] = viewModelScope.launch {
            try {
                val result = media.facets(row, request.second, previous.cursor)
                if (media.session.value == request.first && requests[row] == request) {
                    mutableRows.update { it + (row to FacetRowState((previous.items + result.items).distinctBy(FacetChoice::key),
                        result.nextCursor, result.totalOptions, result.totalMatched)) }
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (media.session.value == request.first && requests[row] == request) {
                    val message = if (failure is ApiFailure) failure.message else "筛选选项读取失败，请重试"
                    mutableRows.update { it + (row to previous.copy(loading = false, error = message)) }
                }
            }
        }
    }
}
