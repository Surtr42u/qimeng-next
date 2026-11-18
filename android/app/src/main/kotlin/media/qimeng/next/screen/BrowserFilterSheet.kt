package media.qimeng.next.screen

import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.ManagementViewModel
import media.qimeng.next.model.VocabularyEntry
import media.qimeng.next.ui.component.*

@Composable
internal fun BrowserFilterSheet(controls: BrowserControls, terms: ManagementViewModel = viewModel(key = "browser-filter-tags")) {
    if (!controls.showFilters.value) return
    var draft by rememberSaveable { mutableStateOf(controls.panel) }
    val tags by terms.entries.collectAsStateWithLifecycle()
    val failure by terms.error.collectAsStateWithLifecycle()
    val busy by terms.busy.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { terms.load("tags") }
    QimengFilterSheet(draft = draft, tags = tags.items.map { FilterTag(it.id, it.name, it.revision) },
        message = failure ?: tags.error, onDraftChange = { draft = it },
        onReset = { controls.panel = FilterPanelDraft(); controls.showFilters.value = false },
        onApply = {
            controls.panel = if (draft.dateRange == PanelDateRange.YEAR_RANGE) draft else draft.copy(yearFrom = null, yearTo = null)
            controls.showFilters.value = false
        },
        onAddTag = { name -> if (!busy) terms.save("tags", null, name, emptyList(), null) {} },
        onDeleteTag = { tag -> if (!busy) terms.deleteTag(VocabularyEntry(tag.id, tag.name, emptyList(), tag.revision)) {
            draft = draft.copy(tagIds = draft.tagIds - tag.id)
            controls.panel = controls.panel.copy(tagIds = controls.panel.tagIds - tag.id)
        } },
        onDismiss = { controls.showFilters.value = false }, hasMoreTags = tags.cursor != null && tags.error == null,
        loadingTags = tags.loading, onLoadMoreTags = { terms.load("tags", next = true) },
        onRetryTags = if (tags.error == null) null else ({ terms.load("tags", next = tags.cursor != null) }))
}
