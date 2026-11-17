package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.ManagementViewModel
import media.qimeng.next.model.VocabularyEntry
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice

@Composable
fun VocabularyScreen(kind: String, model: ManagementViewModel = viewModel()) {
    val state by model.entries.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val error by model.error.collectAsStateWithLifecycle()
    var editing by remember { mutableStateOf<VocabularyEntry?>(null) }
    var showEditor by rememberSaveable(kind) { mutableStateOf(false) }
    var deleting by remember { mutableStateOf<VocabularyEntry?>(null) }
    LaunchedEffect(kind) { model.load(kind) }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { Row {
            Button(onClick = { editing = null; showEditor = true }, enabled = !busy) { Text("添加${vocabularyTitle(kind)}") }
            TextButton(onClick = { model.load(kind) }) { Text("刷新") }
        } }
        if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
        state.error?.let { item { ErrorNotice(it) { model.load(kind) } } }
        error?.let { item { ErrorNotice(it) } }
        if (state.loaded && state.items.isEmpty()) item { Text("尚无${vocabularyTitle(kind)}") }
        items(state.items, key = { it.id }) { entry -> ContentCard {
            Text(entry.name, style = MaterialTheme.typography.titleMedium)
            if (entry.aliases.isNotEmpty()) Text(entry.aliases.joinToString(" · "))
            Row {
                TextButton(onClick = { editing = entry; showEditor = true }, enabled = !busy) { Text("编辑") }
                if (kind == "tags") TextButton(onClick = { deleting = entry }, enabled = !busy) { Text("删除", color = MaterialTheme.colorScheme.error) }
            }
        } }
        if (state.cursor != null) item { TextButton(onClick = { model.load(kind, true) }, enabled = !state.loading) { Text("加载更多") } }
    }
    if (showEditor) VocabularyEditor(kind, editing, model) { showEditor = false }
    deleting?.let { entry -> AlertDialog(onDismissRequest = { deleting = null }, title = { Text("删除标签？") },
        text = { Text("将删除「${entry.name}」及媒体与它的关联，媒体原件保持。") },
        confirmButton = { TextButton(onClick = { model.deleteTag(entry); deleting = null }, enabled = !busy) { Text("删除标签") } },
        dismissButton = { TextButton(onClick = { deleting = null }) { Text("取消") } }) }
}

@Composable
private fun VocabularyEditor(kind: String, initial: VocabularyEntry?, model: ManagementViewModel, close: () -> Unit) {
    var name by rememberSaveable { mutableStateOf(initial?.name.orEmpty()) }
    var aliases by rememberSaveable { mutableStateOf(initial?.aliases?.joinToString("\n").orEmpty()) }
    var source by rememberSaveable { mutableStateOf(initial?.sourceId) }
    val sources by model.sources.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val error by model.error.collectAsStateWithLifecycle()
    LaunchedEffect(kind) { if (kind == "characters") model.loadSources() }
    AlertDialog(onDismissRequest = { if (!busy) close() }, title = { Text("${if (initial == null) "添加" else "编辑"}${vocabularyTitle(kind)}") }, text = {
        Column(Modifier.heightIn(max = 460.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedTextField(name, { name = it }, label = { Text("名称") })
            if (kind != "tags") OutlinedTextField(aliases, { aliases = it }, label = { Text("别名（每行一个）") })
            if (kind == "characters") {
                Text("所属原作 / 出处")
                sources.items.forEach { entry -> Row(verticalAlignment = Alignment.CenterVertically) {
                    RadioButton(source == entry.id, { source = entry.id }); Text(entry.name)
                } }
                if (sources.cursor != null) TextButton(onClick = { model.loadSources(true) }, enabled = !sources.loading) { Text("更多出处") }
                sources.error?.let { ErrorNotice(it) { model.loadSources() } }
            }
            error?.let { ErrorNotice(it) }
        }
    }, confirmButton = { TextButton(onClick = { model.save(kind, initial, name.trim(), aliases.lines().filter(String::isNotBlank), source, close) },
        enabled = !busy && name.isNotBlank() && (kind != "characters" || source != null)) { Text("保存") } },
        dismissButton = { TextButton(onClick = close, enabled = !busy) { Text("取消") } })
}

internal fun vocabularyTitle(kind: String) = when (kind) { "tags" -> "标签"; "sources" -> "原作 / 出处"; "characters" -> "角色"; else -> "词表" }
