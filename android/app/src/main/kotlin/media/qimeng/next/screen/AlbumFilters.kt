package media.qimeng.next.screen

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import media.qimeng.next.FacetRowState
import media.qimeng.next.model.FacetChoice
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.*
import media.qimeng.next.ui.theme.QimengShapes
import media.qimeng.next.ui.theme.softDaylightShadow

private val dimensions = listOf("partitions" to "分区", "works" to "作品", "roles" to "角色", "types" to "类型")

@Composable
internal fun AlbumFilters(title: String, total: Long?, controls: BrowserControls, rows: Map<String, FacetRowState>,
                          onBack: (() -> Unit)?, fetch: (String, Boolean) -> Unit) {
    QimengTitleRow(title, total?.let { "$it 文件" }.orEmpty(), onBack = onBack,
        titleStyle = MaterialTheme.typography.titleLarge.copy(fontSize = 28.sp, lineHeight = 36.sp, fontWeight = FontWeight.Bold),
        onFilterClick = { controls.showFilters.value = true }, filterActive = controls.panel != FilterPanelDraft(),
        columns = controls.columns.intValue, onToggleColumns = { controls.columns.intValue = if (controls.columns.intValue >= 5) 2 else controls.columns.intValue + 1 })
    val shadow = Modifier.softDaylightShadow(QimengShapes.pill)
    QimengChipRow(dimensions.map { (row, label) ->
        val count = rows[row]?.totalOptions?.let { if (row == "partitions" || row == "types") (it - 1).coerceAtLeast(0) else it }
        val text = if (row == "types" && controls.row == row && !controls.expanded)
            "${rows[row]?.items?.find { it.id == (controls.type.value ?: "all") }?.name ?: "全部"} ▼"
        else count?.let { "$label ($it)" } ?: label
        QimengPill(text, controls.row == row)
    }, onPillClick = { index ->
        val next = dimensions[index].first
        controls.expanded = next != controls.row || !controls.expanded
        controls.row = next
    }, dividerBeforeIndex = 3, pillModifier = shadow, modifier = Modifier.padding(horizontal = 16.dp))
    val row = controls.row
    val state = rows[row] ?: return
    if (!controls.expanded) return
    val dynamic = row == "works" || row == "roles"
    val items = when {
        dynamic -> listOfNotNull(state.totalMatched?.let { FacetChoice("all", row, "all", "全部", it) }) + state.items
        row == "types" -> state.items.sortedWith(compareByDescending<FacetChoice> { it.id == "all" }.thenByDescending { it.count })
        else -> state.items
    }
    val selected = when (row) {
        "works" -> controls.works
        "roles" -> controls.roles
        "partitions" -> listOfNotNull(controls.partition.value)
        else -> listOfNotNull(controls.type.value)
    }
    QimengValuePillBlock(items.map { QimengPill("${it.name} (${it.count})", it.key in selected || (it.id == "all" && selected.isEmpty())) },
        onPillClick = { index ->
            val choice = items[index]
            if (dynamic) {
                val next = when { choice.id == "all" -> emptyList(); choice.key in selected -> selected - choice.key; else -> selected + choice.key }
                if (row == "works") controls.works = next else controls.roles = next
            } else {
                val value = choice.id.takeUnless { it == "all" }
                if (row == "types") controls.type.value = value
                else if (controls.partition.value != value) {
                    controls.partition.value = value; controls.works = emptyList(); controls.roles = emptyList()
                }
            }
        }, resetKey = row, pillModifier = shadow,
        loadMore = if (state.cursor != null && !state.loading && state.error == null) ({ fetch(row, true) }) else null)
    state.error?.let { ErrorNotice(it) { fetch(row, state.cursor != null) } }
}
