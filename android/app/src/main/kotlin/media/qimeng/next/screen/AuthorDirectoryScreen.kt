package media.qimeng.next.screen

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.MoreVert
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
            "sources" -> row?.acquisitionSources.orEmpty().joinToString("\n")
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
                    AuthorDirectoryRow(row, busy, { library.openAuthor(row.id) }, { model.follow(row) }, { open(it, row) })
                }
            }
        }
    }
    val mode = dialog ?: return
    val names = draft.split('/').map(String::trim).filter(String::isNotEmpty).distinct()
    val key = rememberSaveable(mode, draft) { UUID.randomUUID().toString() }
    val close = { dialog = null; model.dismissError() }
    AlertDialog(onDismissRequest = { if (!busy) close() }, title = { Text(when (mode) {
        "create" -> "新建作者"; "rename" -> "修改作者名"; "sources" -> "来源 / 出处"; else -> "删除作者" }) },
        text = { Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            error?.let { ErrorNotice(it) }
            if (mode == "delete") Text("删除「${author?.name}」？关联的媒体文件会保留。")
            else if (mode == "sources") OutlinedTextField(draft, { draft = it }, enabled = !busy, label = { Text("获取渠道（每行一条）") })
            else {
                QimengCapsuleTextField(draft, { draft = it }, Modifier.testTag("author-name-draft"), placeholder = "作者名", singleLine = true, enabled = !busy)
                Text("多个别名用「 / 」分隔", style = MaterialTheme.typography.bodySmall)
            }
        } }, confirmButton = { TextButton(onClick = {
            when (mode) {
                "create" -> model.create(names, key, close)
                "rename" -> author?.let { model.rename(it, names, close) }
                "sources" -> author?.let { model.sources(it, draft.lines().map(String::trim).filter(String::isNotEmpty).distinct(), close) }
                "delete" -> author?.let { model.delete(it, close) }
            }
        }, enabled = !busy && (mode !in setOf("create", "rename") || names.isNotEmpty())) { Text(if (mode == "delete") "删除" else "确定") } },
        dismissButton = { TextButton(onClick = close, enabled = !busy) { Text("取消") } })
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
                Row(Modifier.padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (author.kind == "cos") Text("COS", style = MaterialTheme.typography.labelSmall)
                    author.fileCount?.let { Text("$it 个文件", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
            }
            OutlinedButton(onClick = follow, enabled = !busy, modifier = Modifier.height(36.dp), shape = RoundedCornerShape(10.dp), contentPadding = PaddingValues(horizontal = 12.dp)) {
                Text(if (author.followed) "已关注" else "关注", style = MaterialTheme.typography.labelMedium)
            }
            if (author.kind != "cos" && author.id != "unknown") Box {
                IconButton(onClick = { menu = true }, modifier = Modifier.size(36.dp)) { Icon(Icons.Outlined.MoreVert, "作者操作", Modifier.size(20.dp)) }
                DropdownMenu(menu, { menu = false }, modifier = Modifier.width(168.dp), shape = RoundedCornerShape(16.dp)) {
                    listOf("sources" to "来源 / 出处", "rename" to "改名", "delete" to "删除").forEach { (mode, name) ->
                        DropdownMenuItem(text = { Text(name) }, onClick = { menu = false; action(mode) }, enabled = !busy)
                    }
                }
            }
        }
    }
}
