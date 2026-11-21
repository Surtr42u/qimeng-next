package media.qimeng.next.screen

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.AuthorDirectoryQuery
import media.qimeng.next.AuthorDirectoryViewModel
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Author
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.*
import media.qimeng.next.ui.icon.DeleteIcon
import media.qimeng.next.ui.icon.EditPencilIcon
import media.qimeng.next.ui.icon.LabelTagIcon
import media.qimeng.next.ui.icon.MoreVertIcon
import media.qimeng.next.ui.icon.SearchIcon
import java.util.UUID

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun AuthorDirectoryScreen(library: LibraryViewModel, model: AuthorDirectoryViewModel = viewModel()) {
    val target by model.session.collectAsStateWithLifecycle()
    val page by model.rows.collectAsStateWithLifecycle()
    val count by model.allAuthors.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val error by model.error.collectAsStateWithLifecycle()
    var word by rememberSaveable { mutableStateOf("") }
    var kind by rememberSaveable { mutableStateOf<String?>(null) }
    var sort by rememberSaveable { mutableStateOf("default") }
    var menu by remember { mutableStateOf(false) }
    var dialog by rememberSaveable(target?.instanceId) { mutableStateOf<String?>(null) }
    var author by rememberSaveable(target?.instanceId) { mutableStateOf<Author?>(null) }
    var draft by rememberSaveable { mutableStateOf("") }
    val list = rememberLazyListState()
    val query = AuthorDirectoryQuery(word.trim(), kind, sort)
    var visibleQuery by rememberSaveable { mutableStateOf(query.toString()) }
    LaunchedEffect(target, query) {
        target?.let { model.select(it, query) }
        if (visibleQuery != query.toString()) { list.scrollToItem(0); visibleQuery = query.toString() }
    }
    val nearEnd by remember { derivedStateOf { val info = list.layoutInfo
        info.totalItemsCount > 0 && (info.visibleItemsInfo.lastOrNull()?.index ?: -1) >= info.totalItemsCount - 5 } }
    LaunchedEffect(nearEnd, page.cursor) { if (nearEnd && page.error == null) model.load(next = true) }
    val open: (String, Author?) -> Unit = { mode, row ->
        model.dismissError(); author = row
        draft = when (mode) {
            "rename" -> row?.let { (listOf(it.name) + it.aliases).distinct().joinToString(" / ") }.orEmpty()
            else -> ""
        }
        dialog = mode
    }
    Column(Modifier.fillMaxSize()) {
        QimengTopBar("作者管理", onBack = library::back, actions = {
            if (kind != "cos") TextButton(onClick = { open("create", null) }, enabled = !busy) { Text("新建作者") }
        })
        Text(count?.let { "全部作者 · $it 位" } ?: "全部作者", Modifier.padding(start = 16.dp, end = 16.dp, bottom = 12.dp),
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        QimengCapsuleTextField(word, { word = it }, Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, bottom = 8.dp).testTag("author-search"),
            placeholder = "按名字搜索", singleLine = true, leadingIcon = { Icon(SearchIcon, null) })
        Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, bottom = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            val zones = listOf(null to "全部", "regular" to "常规", "cos" to "COS")
            QimengChipRow(zones.map { QimengPill(it.second, kind == it.first) }, { kind = zones[it].first }, Modifier.weight(1f))
            Box {
                QimengSegPill("排序 ▾", false, { menu = true })
                DropdownMenu(menu, { menu = false }, shape = RoundedCornerShape(16.dp), containerColor = MaterialTheme.colorScheme.surface) {
                    listOf("default" to "默认", "views" to "浏览数", "files" to "文件数", "addedAsc" to "加入较早", "addedDesc" to "加入较晚").forEach { (value, label) ->
                        DropdownMenuItem(text = { Text(label) }, onClick = { sort = value; menu = false }, trailingIcon = { if (sort == value) Text("✓") })
                    }
                }
            }
        }
        page.error?.let { ErrorNotice(it) { model.load(next = page.cursor != null) } }
        if (dialog == null) error?.let { ErrorNotice(it) }
        PullToRefreshBox(isRefreshing = page.loading && page.loaded, onRefresh = { model.load() }, modifier = Modifier.weight(1f)) {
            LazyColumn(state = list, contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (page.loaded && page.items.isEmpty()) item { Text("暂无作者", style = MaterialTheme.typography.bodySmall) }
                items(page.items, key = Author::id) { row ->
                    // 点按行已带名字与 fileCount：作为 seed 进入作者作品页，标题/计数首帧即真值。
                    AuthorDirectoryRow(row, busy, { library.openAuthor(row.id, seed = row) }, { model.follow(row) }, { open(it, row) })
                }
            }
        }
    }
    val mode = dialog ?: return
    val close = { dialog = null; model.dismissError() }
    if (mode == "sources") {
        val selected = author ?: return
        val source by model.sourceRecord.collectAsStateWithLifecycle()
        LaunchedEffect(target, selected.id) { model.loadSources(selected.id) }
        AuthorSourcesDialog(
            selected, source, busy, error,
            onRetry = { model.loadSources(selected.id) },
            onSave = { record, values, done -> model.sources(record, values, done) },
            onClose = { model.closeSources(); close() },
        )
        return
    }
    val names = draft.split('/').map(String::trim).filter(String::isNotEmpty).distinct()
    val key = rememberSaveable(mode, draft) { UUID.randomUUID().toString() }
    var confirmDiscard by rememberSaveable { mutableStateOf(false) }
    val initialName = author?.let { (listOf(it.name) + it.aliases).distinct().joinToString(" / ") }.orEmpty()
    val cancel: () -> Unit = {
        if (!busy) {
            if (mode != "delete" && draft != initialName) confirmDiscard = true else close()
        }
    }
    AlertDialog(onDismissRequest = cancel, containerColor = MaterialTheme.colorScheme.surface, title = { Text(when (mode) {
        "create" -> "新建作者"; "rename" -> "修改作者名"; else -> "删除作者" }) },
        text = { Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            error?.let { ErrorNotice(it) }
            if (mode == "delete") Text("删除「${author?.name}」？关联的媒体文件会保留。")
            else {
                QimengCapsuleTextField(draft, { draft = it }, Modifier.testTag("author-name-draft"), placeholder = "作者名", singleLine = true, enabled = !busy)
                Text("多个别名用「 / 」分隔", style = MaterialTheme.typography.bodySmall)
            }
        } }, confirmButton = { TextButton(onClick = {
            when (mode) {
                "create" -> model.create(names, key, close)
                "rename" -> author?.let { model.rename(it, names, close) }
                "delete" -> author?.let { model.delete(it, close) }
            }
        }, enabled = !busy && (mode !in setOf("create", "rename") || names.isNotEmpty())) {
            if (mode == "delete") Text("删除", color = MaterialTheme.colorScheme.error) else Text("确定")
        } }, dismissButton = { TextButton(onClick = cancel, enabled = !busy) { Text("取消") } })
    if (confirmDiscard) AlertDialog(onDismissRequest = { confirmDiscard = false }, title = { Text("放弃未保存的修改？") },
        confirmButton = { TextButton(onClick = { confirmDiscard = false; close() }) { Text("放弃修改") } },
        dismissButton = { TextButton(onClick = { confirmDiscard = false }) { Text("继续编辑") } })
}

@Composable
private fun AuthorDirectoryRow(author: Author, busy: Boolean, open: () -> Unit, follow: () -> Unit, action: (String) -> Unit) {
    var menu by remember { mutableStateOf(false) }
    Surface(shape = RoundedCornerShape(16.dp), color = MaterialTheme.colorScheme.surface,
        border = BorderStroke(0.5.dp, MaterialTheme.colorScheme.outlineVariant.copy(alpha = .35f)), modifier = Modifier.fillMaxWidth()) {
        Row(Modifier.fillMaxWidth().clickable(onClick = open).padding(horizontal = 14.dp, vertical = 12.dp),
            horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
            AuthorAvatar(author.id, author.name)
            Column(Modifier.weight(1f)) {
                Text(author.name, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.SemiBold))
                Row(Modifier.padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                    if (author.kind == "cos") Surface(shape = RoundedCornerShape(6.dp), color = MaterialTheme.colorScheme.surfaceVariant) {
                        Text("COS", Modifier.padding(horizontal = 6.dp, vertical = 2.dp), style = MaterialTheme.typography.labelSmall)
                    }
                    author.fileCount?.let { Text("$it 个文件", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
            }
            OutlinedButton(onClick = follow, enabled = !busy, modifier = Modifier.height(36.dp), shape = RoundedCornerShape(10.dp),
                border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.7f)),
                contentPadding = PaddingValues(horizontal = 12.dp), colors = ButtonDefaults.outlinedButtonColors(
                    contentColor = if (author.followed) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
                )) {
                Text(if (author.followed) "已关注" else "关注", style = MaterialTheme.typography.labelMedium)
            }
            if (author.kind != "cos" && author.id != "unknown") Box {
                IconButton(onClick = { menu = true }, modifier = Modifier.size(36.dp)) {
                    Icon(MoreVertIcon, "${author.name}的作者操作", Modifier.size(20.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                DropdownMenu(menu, { menu = false }, modifier = Modifier.width(168.dp), shape = RoundedCornerShape(16.dp),
                    containerColor = MaterialTheme.colorScheme.surface, tonalElevation = 0.dp, shadowElevation = 6.dp,
                    border = BorderStroke(0.5.dp, MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f))) {
                    DropdownMenuItem(text = { Text("来源 / 出处", style = MaterialTheme.typography.bodyMedium) },
                        leadingIcon = { Icon(LabelTagIcon, null, Modifier.size(18.dp)) }, onClick = { menu = false; action("sources") }, enabled = !busy)
                    DropdownMenuItem(text = { Text("改名", style = MaterialTheme.typography.bodyMedium) },
                        leadingIcon = { Icon(EditPencilIcon, null, Modifier.size(18.dp)) }, onClick = { menu = false; action("rename") }, enabled = !busy)
                    val danger = MaterialTheme.colorScheme.error
                    HorizontalDivider(Modifier.padding(horizontal = 12.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f))
                    DropdownMenuItem(text = { Text("删除", color = danger, style = MaterialTheme.typography.bodyMedium) },
                        leadingIcon = { Icon(DeleteIcon, null, Modifier.size(18.dp), tint = danger) }, onClick = { menu = false; action("delete") }, enabled = !busy)
                }
            }
        }
    }
}
