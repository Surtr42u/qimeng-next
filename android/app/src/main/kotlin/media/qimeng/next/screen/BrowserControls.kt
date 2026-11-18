package media.qimeng.next.screen

import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.ui.component.*

/** Saved presentation choices; server requests remain in LibraryViewModel. */
@Stable
class BrowserControls(partition: String? = null, tag: String = "", columnCount: Int = 3) {
    val type = mutableStateOf<String?>(null)
    val partition = mutableStateOf(partition)
    val fixedTag = tag
    val columns = mutableIntStateOf(columnCount)
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
        it.showFilters.value, it.panel, ArrayList(it.works), ArrayList(it.roles), it.row, it.expanded) },
    restore = { values ->
        if (values.size != 10) null else BrowserControls((values[1] as String).ifBlank { null }, values[2] as String, values[3] as Int).apply {
            type.value = (values[0] as String).ifBlank { null }; showFilters.value = values[4] as Boolean
            panel = values[5] as FilterPanelDraft
            works = (values[6] as List<*>).filterIsInstance<String>(); roles = (values[7] as List<*>).filterIsInstance<String>()
            row = values[8] as String; expanded = values[9] as Boolean
        }
    })

@Composable
fun rememberBrowserControls(key: String, partition: String? = null, tag: String = "", columns: Int = 3): BrowserControls =
    rememberSaveable(key, saver = browserControlsSaver) { BrowserControls(partition, tag, columns) }
