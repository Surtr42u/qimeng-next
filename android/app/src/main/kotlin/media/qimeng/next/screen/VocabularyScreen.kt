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
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
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
    val session by model.session.collectAsStateWithLifecycle()
    val transport by model.transportRevision.collectAsStateWithLifecycle()
    // 编辑对象只保存 id：进程重建后从已载入词表还原，条目不在时关闭编辑器而不是退化成“新建”。
    var editingId by rememberSaveable(kind) { mutableStateOf<String?>(null) }
    var showEditor by rememberSaveable(kind) { mutableStateOf(false) }
    var showGroups by rememberSaveable(kind) { mutableStateOf(false) }
    var deletingId by rememberSaveable(kind) { mutableStateOf<String?>(null) }
    val editing = editingId?.let { id -> state.items.firstOrNull { it.id == id } }
    val deleting = deletingId?.let { id -> state.items.firstOrNull { it.id == id } }
    LaunchedEffect(kind) { model.load(kind) }
    LaunchedEffect(showEditor, editingId, state.loaded, editing) {
        // 恢复时词表可能还在读取：等条目出现再开编辑器；读完后条目已不存在则安全关闭。
        if (showEditor && editingId != null && state.loaded && editing == null) { editingId = null; showEditor = false }
    }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { Row {
            Button(onClick = { editingId = null; showEditor = true }, enabled = !busy) { Text("添加${vocabularyTitle(kind)}") }
            TextButton(onClick = { model.load(kind) }) { Text("刷新") }
        } }
        if (supportsWholeGroupEditing(kind)) item { ContentCard {
            Text("整组编辑", style = MaterialTheme.typography.titleMedium)
            Text("按组维护变体、角色别名与停用词；保存时整份词表一次提交，失败会保留草稿。", style = MaterialTheme.typography.bodySmall)
            TextButton(onClick = { showGroups = true }, enabled = session != null && !busy) { Text("打开整组词表编辑") }
        } }
        if (state.loading && !state.loaded) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
        state.error?.let { item { ErrorNotice(it) { model.load(kind) } } }
        error?.let { item { ErrorNotice(it) } }
        if (state.loaded && state.items.isEmpty()) item { Text("尚无${vocabularyTitle(kind)}") }
        items(state.items, key = { it.id }) { entry -> ContentCard {
            Text(entry.name, style = MaterialTheme.typography.titleMedium)
            if (entry.aliases.isNotEmpty()) Text(entry.aliases.joinToString(" · "))
            Row {
                TextButton(onClick = { editingId = entry.id; showEditor = true }, enabled = !busy) { Text("编辑") }
                if (kind == "tags") TextButton(onClick = { deletingId = entry.id }, enabled = !busy) { Text("删除", color = MaterialTheme.colorScheme.error) }
            }
        } }
        if (state.cursor != null) item { TextButton(onClick = { model.load(kind, true) }, enabled = !state.loading) { Text("加载更多") } }
    }
    // 词表仍在读取且已指定编辑对象时先不弹编辑器，避免以空目标打开成“新建”。
    if (showEditor && (editingId == null || editing != null)) VocabularyEditor(kind, editing, model) { showEditor = false; editingId = null }
    deleting?.let { entry -> AlertDialog(onDismissRequest = { deletingId = null }, title = { Text("删除标签？") },
        text = { Text("将删除「${entry.name}」及媒体与它的关联，媒体原件保持。") },
        confirmButton = { TextButton(onClick = { model.deleteTag(entry); deletingId = null }, enabled = !busy) { Text("删除标签") } },
        dismissButton = { TextButton(onClick = { deletingId = null }) { Text("取消") } }) }
    val target = session
    if (showGroups && target != null) Dialog(onDismissRequest = { showGroups = false },
        properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
            Column(Modifier.fillMaxSize().safeDrawingPadding()) {
                ExtractionVocabularyScreen(target, transport, back = { showGroups = false })
            }
        }
    }
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

/** 整组编辑维护的是原名提取词表（出处组、组内角色与停用词），标签词表不适用。 */
internal fun supportsWholeGroupEditing(kind: String) = kind == "sources" || kind == "characters"
