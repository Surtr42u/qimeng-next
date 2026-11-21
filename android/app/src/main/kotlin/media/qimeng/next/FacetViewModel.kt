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
    /** 每行当前候选来自哪个实例：换服务器时旧计数属于另一座库，不能继续显示。 */
    private val rowInstances = mutableMapOf<String, String>()
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
        // 实例变了（换服务器）旧行的候选与计数属于另一座库，必须丢弃；同一实例内的重查沿用。
        val sameInstance = rowInstances[row] == request.first.instanceId
        rowInstances[row] = request.first.instanceId
        // 首屏重查（换筛选/重试）沿用上一行候选与计数：旧版 loadFacets() 在响应回来前保留
        // partitionOptions/…/totalForAllPill，成功才整份覆盖。清空会让胶囊数字消失、值区块
        // 塌成 0 高（表头二次回流），这正是「计数先消失再出现」的来源。只有翻页才要求已有行。
        val previous = if (next) mutableRows.value[row]?.takeIf { sameInstance } ?: return
            else mutableRows.value[row]?.takeIf { sameInstance } ?: FacetRowState()
        if (next && (previous.loading || previous.cursor == null)) return
        jobs.remove(row)?.cancel()
        mutableRows.update { it + (row to previous.copy(loading = true, error = null)) }
        jobs[row] = viewModelScope.launch {
            try {
                // 首屏重查必须从游标 null 开始；沿用旧行只为了显示，请求参数不能跟着沿用。
                val result = media.facets(row, request.second, if (next) previous.cursor else null)
                if (media.session.value == request.first && requests[row] == request) {
                    val items = if (next) (previous.items + result.items).distinctBy(FacetChoice::key) else result.items
                    mutableRows.update { it + (row to FacetRowState(items, result.nextCursor, result.totalOptions, result.totalMatched)) }
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (media.session.value == request.first && requests[row] == request) {
                    val message = if (failure is ApiFailure) failure.message else "筛选选项读取失败，请重试"
                    // 失败也保留上一份真实候选与计数（页内另有错误提示），不回退成空白。
                    mutableRows.update { it + (row to previous.copy(loading = false, error = message)) }
                }
            }
        }
    }
}
