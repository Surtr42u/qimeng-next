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
            SettingsRow("服务器", session?.label.orEmpty(), Icons.Outlined.Dns) { model.navigate("server") }
        }
        ContentCard(Modifier.padding(horizontal = 16.dp)) {
            Text("使用偏好", style = MaterialTheme.typography.titleSmall)
            SettingsRow("外观", "${themeName(preferences.theme)} · ${if (preferences.glass) "玻璃导航" else "实色导航"}", Icons.Outlined.Palette) { model.navigate("appearance") }
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
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        ContentCard {
            Text("当前连接", style = MaterialTheme.typography.titleMedium)
            Text(session?.label.orEmpty()); Text(session?.baseUrl.orEmpty())
            Text("切换服务器前，现有传输始终保留原来的目标。", style = MaterialTheme.typography.bodySmall)
            OutlinedButton(onClick = model::refreshConnection) { Text("检查连接") }
        }
        OutlinedButton(onClick = { confirm = true }) { Text("退出并切换服务器") }
    }
    if (confirm) AlertDialog(onDismissRequest = { confirm = false }, title = { Text("退出当前登录？") },
        text = { Text("将撤销当前会话。尚未发送的传输保留，重新连接原服务器后可重试。") },
        confirmButton = { TextButton(onClick = { confirm = false; model.logout() }) { Text("退出登录") } },
        dismissButton = { TextButton(onClick = { confirm = false }) { Text("取消") } })
}

@Composable
fun AuthorsScreen(model: LibraryViewModel) {
    val state by model.authors.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (!state.loaded) model.loadAuthors() }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { TextButton(onClick = { model.loadAuthors() }) { Text("刷新作者") } }
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
}

@Composable
fun AuthorDetailScreen(model: LibraryViewModel, id: String) {
    val author by model.author.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    LaunchedEffect(id) { if (author?.id != id) model.openAuthor(id) }
    val item = author?.takeIf { it.id == id }
    if (item == null) { TextButton(onClick = { model.openAuthor(id) }, enabled = !busy) { Text("读取作者 · 重试") }; return }
    Column(Modifier.fillMaxSize()) {
        ContentCard(Modifier.padding(horizontal = 16.dp)) {
            Text(item.name, style = MaterialTheme.typography.headlineSmall)
            if (item.aliases.isNotEmpty()) Text(item.aliases.joinToString(" / "))
            if (item.biography.isNotBlank()) Text(item.biography, Modifier.heightIn(max = 180.dp).verticalScroll(rememberScrollState()))
            item.sources.forEach { Text(it, style = MaterialTheme.typography.bodySmall) }
            OutlinedButton(onClick = model::followAuthor, enabled = !busy) { Text(if (item.followed) "取消关注" else "关注作者") }
        }
        MediaBrowser(model, "author:$id", author = id)
    }
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

@Composable
fun StatisticsScreen(model: LibraryViewModel) {
    val stats by model.statistics.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (stats == null) model.loadStatistics() }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = 128.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        PageHeader("数据", actions = { IconButton(onClick = model::loadStatistics) { Icon(Icons.Outlined.Refresh, "刷新统计") } })
        ContentCard(Modifier.padding(horizontal = 16.dp)) {
            Text("当前影库", style = MaterialTheme.typography.titleMedium)
            if (stats == null) Text("统计尚未读取") else stats!!.let { value ->
                Text("${value.assets} 件媒体", style = MaterialTheme.typography.headlineMedium)
                Text("图片 ${value.images} · 视频 ${value.videos}")
                Text("原件 ${formatBytes(value.bytes)}")
                HorizontalDivider()
                Text("收藏 ${value.favorites} · 作者 ${value.authors}")
                Text("浏览事件 ${value.events} · 回收站 ${value.trash}")
            }
        }
        ContentCard(Modifier.padding(horizontal = 16.dp)) {
            SettingsRow("浏览历史", "查看逐次记录与播放位置", Icons.Outlined.History) { model.navigate("history") }
            SettingsRow("任务中心", "查看当前服务器持久报告", Icons.Outlined.Assignment) { model.navigate("reports") }
        }
    }
}

private fun themeName(theme: ThemeChoice): String = when (theme) { ThemeChoice.SYSTEM -> "跟随系统"; ThemeChoice.LIGHT -> "浅色"; ThemeChoice.DARK -> "深色" }
