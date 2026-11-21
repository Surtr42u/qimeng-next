package media.qimeng.next.screen

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.ListState
import media.qimeng.next.UploadDirectoryViewModel
import media.qimeng.next.model.Directory
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.*

@Composable
internal fun UploadPanel(title: String, subtitle: String? = null, content: @Composable ColumnScope.() -> Unit) {
    QimengSettingsCard {
        Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
            subtitle?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            content()
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun UploadDestinationSection(model: LibraryViewModel, tree: UploadDirectoryViewModel = viewModel()) {
    val session by model.session.collectAsStateWithLifecycle()
    val list by model.libraryPage.collectAsStateWithLifecycle()
    val selected by model.uploadLibrary.collectAsStateWithLifecycle()
    val directory by model.uploadDirectory.collectAsStateWithLifecycle()
    val branches by tree.branches.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    var create by rememberSaveable(selected) { mutableStateOf(false) }
    val enabled = list.items.any { it.enabled && it.id == selected }
    LaunchedEffect(session, selected, enabled) {
        if (!enabled) tree.clear() else session?.let { tree.bind(it, selected, directory) }
    }
    DisposableEffect(tree) { onDispose { tree.clear() } }
    UploadPanel("上传位置", "选择目标媒体库；未选择目录时上传到库根。") {
        if (list.loading && !list.loaded) LinearProgressIndicator(Modifier.fillMaxWidth())
        if (list.error != null) ErrorNotice("媒体库列表读取失败，不能据此判断没有媒体库。")
        if (list.loaded && list.error == null && list.items.none { it.enabled }) {
            Text("还没有可选的媒体库", style = MaterialTheme.typography.titleSmall)
            Text("先在库管理登记一个目录，再回来刷新列表。", style = MaterialTheme.typography.bodySmall)
            OutlinedButton(onClick = { model.navigate("libraries") }) { Text("管理媒体库") }
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            list.items.filter { it.enabled }.forEach { library ->
                QimengSegPill(library.name, library.id == selected, { model.setUploadLibrary(library.id) }, enabled = !busy)
            }
            if (list.cursor != null) TextButton(onClick = { model.loadLibraries(next = true) }, enabled = !list.loading) { Text("更多媒体库") }
        }
        TextButton(onClick = { model.loadLibraries() }, enabled = !list.loading) { Text(if (list.loading) "正在读取媒体库…" else "刷新列表") }
        if (enabled) {
            Text("目标目录", style = MaterialTheme.typography.titleSmall)
            Column(Modifier.fillMaxWidth().padding(8.dp)) {
                val root = branches[""]
                DirectoryRow("（库根）", root?.totalMatched, directory.isEmpty(), 0, null, { model.setUploadDirectory("") })
                DirectoryChildren("", 1, directory, branches, tree, model::setUploadDirectory)
            }
            Text("已选目录：${directory.ifEmpty { "（库根）" }}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedButton(onClick = { create = true }, enabled = !busy) { Text("新建子目录") }
        }
    }
    if (create && enabled) TextEditDialog("新建子目录", "", busy, { create = false }) { name ->
        model.createDirectory(selected, directory, name) { create = false; tree.open(directory) }
    }
}

@Composable
private fun DirectoryChildren(parent: String, depth: Int, selected: String, branches: Map<String, ListState<Directory>>,
                              tree: UploadDirectoryViewModel, select: (String) -> Unit) {
    val branch = branches[parent] ?: return
    branch.items.forEach { item -> key(item.path) {
        val page = branches[item.path]
        val expanded = page != null
        val hasChildren = page?.takeIf { it.loaded }?.let { it.items.isNotEmpty() } ?: item.hasChildren
        val toggle = { if (expanded) tree.collapse(item.path) else tree.open(item.path) }
        DirectoryRow(item.name, item.fileCount, item.path == selected, depth,
            if (hasChildren == false) null else expanded, { select(item.path); if (hasChildren != false && !expanded) tree.open(item.path) }, toggle)
        if (expanded) DirectoryChildren(item.path, depth + 1, selected, branches, tree, select)
    } }
    branch.error?.let { ErrorNotice(it) { tree.open(parent, next = branch.cursor != null) } }
    if (branch.cursor != null) TextButton(onClick = { tree.open(parent, next = true) }, enabled = !branch.loading) { Text("更多目录") }
}

@Composable
private fun DirectoryRow(label: String, count: Long?, selected: Boolean, depth: Int, expanded: Boolean?,
                         select: () -> Unit, toggle: () -> Unit = {}) {
    Row(Modifier.fillMaxWidth().clickable(onClick = select).padding(start = (depth.coerceAtMost(8) * 20).dp, top = 6.dp, bottom = 6.dp),
        verticalAlignment = Alignment.CenterVertically) {
        if (expanded == null) Spacer(Modifier.width(48.dp))
        else Box(Modifier.size(width = 48.dp, height = 32.dp).clickable(onClick = toggle), contentAlignment = Alignment.Center) { Text(if (expanded) "▾" else "▸") }
        Text(if (selected) "● $label" else "○ $label", Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium)
        count?.let { Text("$it 个文件", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}
