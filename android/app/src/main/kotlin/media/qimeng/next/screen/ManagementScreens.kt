package media.qimeng.next.screen

import android.os.Build
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.ThemeChoice
import media.qimeng.next.ui.*

@Composable
fun ProfileScreen(model: LibraryViewModel) {
    val session by model.session.collectAsStateWithLifecycle()
    val preferences by model.preferences.collectAsStateWithLifecycle()
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = 128.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        PageHeader("我的")
        ContentCard(Modifier.padding(horizontal = 16.dp)) {
            Text("我的内容", style = MaterialTheme.typography.titleSmall)
            SettingsRow("收藏", "保存喜欢的作品", Icons.Outlined.Bookmark) { model.navigate("favorites") }
            SettingsRow("浏览历史", "继续上次播放与浏览", Icons.Outlined.History) { model.navigate("history") }
            SettingsRow("作者", "作者档案与关注", Icons.Outlined.People) { model.navigate("authors") }
        }
        ContentCard(Modifier.padding(horizontal = 16.dp)) {
            Text("影库与连接", style = MaterialTheme.typography.titleSmall)
            SettingsRow("上传新文件", "系统文件、分享与持久传输队列", Icons.Outlined.Upload) { model.navigate("upload") }
            SettingsRow("任务中心", "智能整理、逐项结果与等待原因", Icons.Outlined.Assignment) { model.navigate("reports") }
            SettingsRow("回收站", "恢复误删的媒体", Icons.Outlined.DeleteOutline) { model.navigate("trash") }
            SettingsRow("标签", "维护词表与媒体关联", Icons.Outlined.Label) { model.navigate("vocabulary:tags") }
            SettingsRow("原作 / 出处", "维护名称与别名", Icons.Outlined.MenuBook) { model.navigate("vocabulary:sources") }
            SettingsRow("角色", "维护角色及所属出处", Icons.Outlined.Badge) { model.navigate("vocabulary:characters") }
            SettingsRow("服务器", session?.label.orEmpty(), Icons.Outlined.Dns) { model.navigate("server") }
        }
        ContentCard(Modifier.padding(horizontal = 16.dp)) {
            Text("使用偏好", style = MaterialTheme.typography.titleSmall)
            SettingsRow("外观", "${themeName(preferences.theme)} · ${if (preferences.glass) "玻璃导航" else "实色导航"}", Icons.Outlined.Palette) { model.navigate("appearance") }
            SettingsRow("推荐偏好", "选择适合你的浏览方式", Icons.Outlined.Tune) { model.navigate("recommendation") }
        }
        ContentCard(Modifier.padding(horizontal = 16.dp)) {
            Text("绮梦 Next · 0.1.0-dev", style = MaterialTheme.typography.titleSmall)
            Text("媒体与业务数据由当前连接的服务器保存。", style = MaterialTheme.typography.bodySmall)
        }
    }
}

@Composable
private fun SettingsRow(title: String, subtitle: String, icon: ImageVector, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(icon, null, Modifier.padding(end = 16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(title, style = MaterialTheme.typography.titleMedium)
            Text(subtitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Icon(Icons.Outlined.ChevronRight, null)
    }
}

@Composable
fun AppearanceScreen(model: LibraryViewModel) {
    val preferences by model.preferences.collectAsStateWithLifecycle()
    val capability = glassCapability(Build.VERSION.SDK_INT, LocalView.current.isHardwareAccelerated, preferences.glass)
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).navigationBarsPadding().padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        ContentCard {
            Text("主题", style = MaterialTheme.typography.titleMedium)
            Column(Modifier.selectableGroup()) {
                ThemeChoice.entries.forEach { theme ->
                    Row(Modifier.fillMaxWidth().selectable(preferences.theme == theme, role = Role.RadioButton,
                        onClick = { model.savePreferences(preferences.copy(theme = theme)) }).padding(vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                        RadioButton(preferences.theme == theme, null)
                        Text(themeName(theme), Modifier.padding(start = 12.dp))
                    }
                }
            }
        }
        ContentCard {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) { Text("玻璃导航"); Text(capability.description, style = MaterialTheme.typography.bodySmall) }
                Switch(preferences.glass, { model.savePreferences(preferences.copy(glass = it)) })
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) { Text("减少动效"); Text("立即切换页面与选中状态", style = MaterialTheme.typography.bodySmall) }
                Switch(preferences.reducedMotion, { model.savePreferences(preferences.copy(reducedMotion = it)) })
            }
            Text("系统关闭动画时也会减少动效。字体大小跟随系统设置。", style = MaterialTheme.typography.bodySmall)
        }
    }
}

@Composable
fun ServerScreen(model: LibraryViewModel) {
    val session by model.session.collectAsStateWithLifecycle()
    var confirm by rememberSaveable { mutableStateOf(false) }
    var create by rememberSaveable { mutableStateOf(false) }
    var disconnect by rememberSaveable { mutableStateOf(false) }
    val libraries by model.libraries.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        ContentCard {
            Text("当前连接", style = MaterialTheme.typography.titleMedium)
            Text(session?.label.orEmpty()); Text(session?.baseUrl.orEmpty())
            Text("切换服务器前，现有传输始终保留原来的目标。", style = MaterialTheme.typography.bodySmall)
            OutlinedButton(onClick = model::refreshConnection) { Text("检查连接") }
        }
        ContentCard {
            Text("影库", style = MaterialTheme.typography.titleMedium)
            libraries.forEach { library ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(library.name, Modifier.weight(1f))
                    Text(if (library.enabled) "接收上传" else "已暂停", style = MaterialTheme.typography.bodySmall)
                    Switch(library.enabled, { model.updateLibrary(library, enabled = it) }, enabled = !busy)
                }
            }
            TextButton(onClick = { create = true }) { Text("创建影库") }
        }
        OutlinedButton(onClick = { confirm = true }) { Text("退出并切换服务器") }
        TextButton(onClick = { disconnect = true }) { Text("服务器离线时，仅断开本机") }
    }
    if (confirm) AlertDialog(onDismissRequest = { confirm = false }, title = { Text("退出当前登录？") },
        text = { Text("将撤销当前会话。尚未发送的传输保留，重新连接原服务器后可重试。") },
        confirmButton = { TextButton(onClick = { confirm = false; model.logout() }) { Text("退出登录") } },
        dismissButton = { TextButton(onClick = { confirm = false }) { Text("取消") } })
    if (disconnect) AlertDialog(onDismissRequest = { disconnect = false }, title = { Text("仅断开本机连接？") },
        text = { Text("清除本机凭据后可以登录其他服务器。此操作无法撤销离线服务器上的会话，服务器恢复后可在服务端撤销。") },
        confirmButton = { TextButton(onClick = { disconnect = false; model.disconnect() }) { Text("断开本机") } },
        dismissButton = { TextButton(onClick = { disconnect = false }) { Text("取消") } })
    if (create) {
        var name by rememberSaveable { mutableStateOf("") }
        var cos by rememberSaveable { mutableStateOf(false) }
        AlertDialog(onDismissRequest = { if (!busy) create = false }, title = { Text("创建影库") }, text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                OutlinedTextField(name, { name = it }, label = { Text("影库名称") })
                Row(verticalAlignment = Alignment.CenterVertically) { Checkbox(cos, { cos = it }); Text("COS 影库") }
            }
        }, confirmButton = { TextButton(onClick = { model.createLibrary(name.trim(), cos) { create = false } }, enabled = !busy && name.isNotBlank()) { Text("创建") } },
            dismissButton = { TextButton(onClick = { create = false }, enabled = !busy) { Text("取消") } })
    }
}

@Composable
fun AuthorsScreen(model: LibraryViewModel) {
    val state by model.authors.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    var creating by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(Unit) { if (!state.loaded) model.loadAuthors() }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { Row {
            TextButton(onClick = { model.loadAuthors() }) { Text("刷新作者") }
            TextButton(onClick = { creating = true }) { Text("添加作者") }
        } }
        state.error?.let { item { ErrorNotice(it) { model.loadAuthors() } } }
        if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
        if (state.loaded && state.items.isEmpty()) item { Text("尚无作者") }
        items(state.items, key = { it.id }) { author ->
            ContentCard(Modifier.fillMaxWidth().clickable { model.openAuthor(author.id) }) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(author.name.take(1), Modifier.padding(end = 18.dp), style = MaterialTheme.typography.headlineMedium)
                    Column(Modifier.weight(1f)) { Text(author.name, style = MaterialTheme.typography.titleMedium); if (author.aliases.isNotEmpty()) Text(author.aliases.joinToString(" · "), style = MaterialTheme.typography.bodySmall) }
                    if (author.followed) Text("已关注", color = MaterialTheme.colorScheme.primary)
                }
            }
        }
        if (state.cursor != null) item { TextButton(onClick = { model.loadAuthors(true) }, enabled = !state.loading) { Text("加载更多作者") } }
    }
    if (creating) TextEditDialog("添加常规作者", "", busy, { creating = false }) { model.createAuthor(it.trim()) { creating = false } }
}

@Composable
fun AuthorDetailScreen(model: LibraryViewModel, id: String) {
    val author by model.author.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    var edit by rememberSaveable(id) { mutableStateOf(false) }
    var deleting by rememberSaveable(id) { mutableStateOf(false) }
    var merging by rememberSaveable(id) { mutableStateOf(false) }
    LaunchedEffect(id) { if (author?.id != id) model.openAuthor(id) }
    val item = author?.takeIf { it.id == id }
    if (item == null) { TextButton(onClick = { model.openAuthor(id) }, enabled = !busy) { Text("读取作者 · 重试") }; return }
    Column(Modifier.fillMaxSize()) {
        ContentCard(Modifier.padding(horizontal = 16.dp)) {
            Text(item.name, style = MaterialTheme.typography.headlineSmall)
            if (item.aliases.isNotEmpty()) Text(item.aliases.joinToString(" / "))
            if (item.biography.isNotBlank()) Text(item.biography, Modifier.heightIn(max = 180.dp).verticalScroll(rememberScrollState()))
            item.sources.forEach { Text(it, style = MaterialTheme.typography.bodySmall) }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = model::followAuthor, enabled = !busy) { Text(if (item.followed) "取消关注" else "关注作者") }
                if (item.kind != "cos") TextButton(onClick = { edit = true }, enabled = !busy) { Text("编辑档案") }
            }
            if (item.kind != "cos") Row {
                TextButton(onClick = { model.loadAuthors(); merging = true }, enabled = !busy) { Text("合并重复作者") }
                TextButton(onClick = { deleting = true }, enabled = !busy) { Text("删除作者", color = MaterialTheme.colorScheme.error) }
            }
        }
        MediaBrowser(model, "author:$id", author = id)
    }
    if (edit) {
        var name by rememberSaveable { mutableStateOf(item.name) }
        var aliases by rememberSaveable { mutableStateOf(item.aliases.joinToString("\n")) }
        var biography by rememberSaveable { mutableStateOf(item.biography) }
        var homepage by rememberSaveable { mutableStateOf(item.sources.firstOrNull().orEmpty()) }
        val message by model.message.collectAsStateWithLifecycle()
        AlertDialog(onDismissRequest = { if (!busy) edit = false }, title = { Text("编辑作者档案") }, text = {
            Column(Modifier.heightIn(max = 460.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                OutlinedTextField(name, { name = it }, label = { Text("名称") })
                OutlinedTextField(aliases, { aliases = it }, label = { Text("别名（每行一个）") })
                OutlinedTextField(homepage, { homepage = it }, label = { Text("主页 / 来源") })
                OutlinedTextField(biography, { biography = it }, label = { Text("简介") }, minLines = 3)
                message?.let { ErrorNotice(it) }
            }
        }, confirmButton = { TextButton(onClick = { model.editAuthor(name.trim(), aliases.lines().filter(String::isNotBlank), biography, homepage) { edit = false } }, enabled = !busy && name.isNotBlank()) { Text("保存") } },
            dismissButton = { TextButton(onClick = { edit = false }, enabled = !busy) { Text("取消") } })
    }
    if (deleting) AlertDialog(onDismissRequest = { deleting = false }, title = { Text("删除作者？") },
        text = { Text("删除「${item.name}」的档案与关联；媒体原件和其他作者保留。") },
        confirmButton = { TextButton(onClick = { deleting = false; model.deleteCurrentAuthor() }, enabled = !busy) { Text("删除作者") } },
        dismissButton = { TextButton(onClick = { deleting = false }) { Text("取消") } })
    if (merging) {
        val candidates by model.authors.collectAsStateWithLifecycle()
        var selected by rememberSaveable { mutableStateOf<String?>(null) }
        val target = candidates.items.find { it.id == selected }
        AlertDialog(onDismissRequest = { if (!busy) merging = false }, title = { Text("合并「${item.name}」") }, text = {
            Column(Modifier.heightIn(max = 420.dp)) {
                Text("选择保留的目标作者。源作者的媒体关联将移入目标，目标档案受保护。")
                LazyColumn(Modifier.weight(1f, fill = false)) {
                    items(candidates.items.filter { it.id != item.id && it.kind == "regular" }, key = { it.id }) { candidate ->
                        Row(verticalAlignment = Alignment.CenterVertically) { RadioButton(selected == candidate.id, { selected = candidate.id }); Text(candidate.name) }
                    }
                    if (candidates.cursor != null) item { TextButton(onClick = { model.loadAuthors(true) }, enabled = !candidates.loading) { Text("更多作者") } }
                }
            }
        }, confirmButton = { TextButton(onClick = { target?.let { model.mergeCurrentAuthor(it) { merging = false } } }, enabled = !busy && target != null) { Text("合并到 ${target?.name.orEmpty()}") } },
            dismissButton = { TextButton(onClick = { merging = false }, enabled = !busy) { Text("取消") } })
    }
}

@Composable
fun TrashScreen(model: LibraryViewModel) {
    val state by model.trash.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    var purgeId by rememberSaveable { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) { if (!state.loaded) model.loadTrash() }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { Text("已移入回收站的原件可以恢复。") }
        item { TextButton(onClick = { model.loadTrash() }) { Text("刷新回收站") } }
        state.error?.let { item { ErrorNotice(it) { model.loadTrash() } } }
        if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
        if (state.loaded && state.items.isEmpty()) item { Text("回收站为空") }
        items(state.items, key = { it.id }) { asset -> ContentCard {
            Text(asset.name, style = MaterialTheme.typography.titleMedium)
            Text(formatBytes(asset.bytes))
            OutlinedButton(onClick = { model.restoreAsset(asset) }, enabled = !busy) { Text("恢复媒体") }
            TextButton(onClick = { purgeId = asset.id }, enabled = !busy) { Text("永久删除", color = MaterialTheme.colorScheme.error) }
        } }
        if (state.cursor != null) item { TextButton(onClick = { model.loadTrash(true) }, enabled = !state.loading) { Text("加载更多") } }
    }
    state.items.find { it.id == purgeId }?.let { asset -> AlertDialog(onDismissRequest = { purgeId = null }, title = { Text("永久删除回收站原件？") },
        text = { Text("${asset.name}\n此操作会删除回收站原件，不能通过本回收站恢复。") },
        confirmButton = { TextButton(onClick = { model.purgeAsset(asset); purgeId = null }, enabled = !busy) { Text("永久删除", color = MaterialTheme.colorScheme.error) } },
        dismissButton = { TextButton(onClick = { purgeId = null }) { Text("取消") } }) }
}

@Composable
fun HistoryScreen(model: LibraryViewModel) {
    val state by model.history.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (!state.loaded) model.loadHistory() }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        item { TextButton(onClick = { model.loadHistory() }) { Text("刷新历史") } }
        state.error?.let { item { ErrorNotice(it) { model.loadHistory() } } }
        if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
        if (state.loaded && state.items.isEmpty()) item { Text("还没有浏览记录") }
        items(state.items, key = { it.id }) { entry ->
            ContentCard(Modifier.fillMaxWidth().clickable { model.openAsset(entry.assetId) }) {
                Text(entry.name, style = MaterialTheme.typography.titleMedium)
                Text("${entry.createdAt} · ${if (entry.kind == "view") "浏览" else "播放 ${entry.positionMs / 1000} 秒"}", style = MaterialTheme.typography.bodySmall)
            }
        }
        if (state.cursor != null) item { TextButton(onClick = { model.loadHistory(true) }, enabled = !state.loading) { Text("加载更多记录") } }
    }
}

private fun themeName(theme: ThemeChoice): String = when (theme) { ThemeChoice.SYSTEM -> "跟随系统"; ThemeChoice.LIGHT -> "浅色"; ThemeChoice.DARK -> "深色" }
