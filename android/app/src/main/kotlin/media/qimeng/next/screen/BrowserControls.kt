package media.qimeng.next.screen

import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.ui.component.*

/** 网格列数档位（MediaBrowser 列数循环、捏合缩放与首页顶栏切换共用）：2~5 列。 */
internal const val MIN_GRID_COLUMNS = 2
internal const val MAX_GRID_COLUMNS = 5
/**
 * 首页单列档：首页顶栏是 1 列 ↔ 2 列切换（旧版 KEY_HOME_COLUMNS 同样允许 1 列），
 * 网格捏合仍只在 2~5 列之间调整。持久化必须覆盖 1，否则重启会把用户选的单列截成两列。
 */
internal const val HOME_MIN_GRID_COLUMNS = 1

/** Saved presentation choices; server requests remain in LibraryViewModel. */
@Stable
class BrowserControls(
    partition: String? = null,
    tag: String = "",
    columnCount: Int = 3,
    /** 恢复列数下界（首页 1，其余网格 2）；Saver 需要读它，故模块内可见。 */
    internal val minColumns: Int = MIN_GRID_COLUMNS,
) {
    val type = mutableStateOf<String?>(null)
    val partition = mutableStateOf(partition)
    val fixedTag = tag
    val columns = mutableIntStateOf(columnCount.coerceIn(minColumns, MAX_GRID_COLUMNS))
    val showFilters = mutableStateOf(false)
    var panel by mutableStateOf(FilterPanelDraft())
    var works by mutableStateOf(emptyList<String>())
    var roles by mutableStateOf(emptyList<String>())
    var row by mutableStateOf("partitions")
    var expanded by mutableStateOf(false)

    fun query(base: AssetQuery): AssetQuery {
        val years = listOfNotNull(panel.yearFrom, panel.yearTo).takeIf { panel.dateRange == PanelDateRange.YEAR_RANGE }
        val filteredCos = base.source == "recommendations" && base.kind == "cos" && panel != FilterPanelDraft()
        return base.copy(kind = base.kind ?: partition.value, mediaType = type.value, tag = fixedTag.ifBlank { null },
            source = if (filteredCos) "assets" else base.source, workKeys = works, roleKeys = roles,
            sort = when (panel.sort) { FilterSort.DEFAULT -> base.sort; FilterSort.VIEW_COUNT -> "views"; FilterSort.SIZE_BYTES -> "size" },
            order = panel.order.name.lowercase(), viewRange = panel.viewRange.name.lowercase(), playRange = panel.playRange.name.lowercase(),
            sizeRange = when (panel.sizeRange) { PanelSizeRange.ALL -> "all"; PanelSizeRange.LT_1M -> "lt1m"
                PanelSizeRange.M_1_TO_10 -> "m1to10"; PanelSizeRange.M_10_TO_50 -> "m10to50"; PanelSizeRange.GT_50M -> "gt50m" },
            timeRange = if (panel.dateRange == PanelDateRange.QUARTER) "three_months" else panel.dateRange.name.lowercase(),
            fromYear = years?.minOrNull(), toYear = years?.maxOrNull(),
            tags = panel.tagIds, tagMode = if (panel.tagMode == PanelTagMode.EXACT) "all" else "any")
    }
}

private val browserControlsSaver = listSaver<BrowserControls, Any>(
    save = { listOf(it.type.value.orEmpty(), it.partition.value.orEmpty(), it.fixedTag, it.columns.intValue,
        it.showFilters.value, it.panel, ArrayList(it.works), ArrayList(it.roles), it.row, it.expanded,
        it.minColumns.coerceAtLeast(HOME_MIN_GRID_COLUMNS)) },
    restore = { values ->
        // 第 11 位是恢复列数下界（首页允许 1 列，其余网格 2 列起）；旧格式没有这一位，按网格默认。
        if (values.size != 11) null else BrowserControls(
            (values[1] as String).ifBlank { null }, values[2] as String, values[3] as Int, values[10] as Int,
        ).apply {
            type.value = (values[0] as String).ifBlank { null }; showFilters.value = values[4] as Boolean
            panel = values[5] as FilterPanelDraft
            works = (values[6] as List<*>).filterIsInstance<String>(); roles = (values[7] as List<*>).filterIsInstance<String>()
            row = values[8] as String; expanded = values[9] as Boolean
        }
    })

@Composable
fun rememberBrowserControls(
    key: String,
    partition: String? = null,
    tag: String = "",
    columns: Int = 3,
    minColumns: Int = MIN_GRID_COLUMNS,
): BrowserControls =
    rememberSaveable(key, saver = browserControlsSaver) { BrowserControls(partition, tag, columns, minColumns) }
