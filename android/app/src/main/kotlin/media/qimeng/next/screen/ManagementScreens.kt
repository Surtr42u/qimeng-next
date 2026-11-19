package media.qimeng.next.screen

import android.os.Build
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.LoadState
import media.qimeng.next.model.Author
import media.qimeng.next.model.Session
import media.qimeng.next.model.ThemeChoice
import media.qimeng.next.ui.*
import media.qimeng.next.ui.component.*
import media.qimeng.next.ui.icon.*
import media.qimeng.next.ui.theme.softDaylightShadow
import media.qimeng.next.ui.theme.isQimengDarkTheme

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ProfileScreen(model: LibraryViewModel) {
    val preferences by model.preferences.collectAsStateWithLifecycle()
    val inventory by model.statistics.collectAsStateWithLifecycle()
    val recommendation by model.recommendationPreferences.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    var confirmLogout by rememberSaveable { mutableStateOf(false) }
    var showPreferences by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(Unit) { model.loadStatistics(); if (recommendation == null) model.loadRecommendationPreferences() }
    // Presentation hierarchy and dimensions retained from the existing SettingsScreen.
    val scroll = androidx.compose.foundation.lazy.rememberLazyListState()
    LaunchedEffect(scroll) { TabScrollController.events.collect { if (it == 3) scroll.scrollToItem(0) } }
    LazyColumn(Modifier.fillMaxSize().testTag("profile-settings-list"), state = scroll, contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = media.qimeng.next.ui.glass.TabDockDefaults.bottomClearance()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        item { Column(Modifier.padding(top = 12.dp)) {
            Text("我的", style = MaterialTheme.typography.headlineLarge, fontWeight = FontWeight.Bold)
            QimengPageIntro("你的收藏、浏览足迹与影库偏好")
            Text("当前媒体库", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(bottom = 8.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                InventoryCountCard("图片", inventory?.images, Modifier.weight(1f))
                InventoryCountCard("视频", inventory?.videos, Modifier.weight(1f))
            }
        } }
        item { QimengSettingsGroup("我的内容") {
            QimengSettingsRow(FavoriteBookmarkIcon, "收藏", "留住喜欢的图片和视频", { model.navigate("favorites") })
            QimengSettingsRow(HistoryRecentIcon, "浏览历史", "接着查看最近打开的作品", { model.navigate("history") })
            QimengSettingsRow(AuthorPeopleIcon, "作者管理", "名称、别名、来源与关注", { model.navigate("authors") })
        } }
        item { QimengSettingsGroup("影库与连接") {
            QimengSettingsRow(CloudUploadIcon, "数据管理", "上传、媒体库、词表与备份", { model.navigate("data-manage") })
            QimengSettingsRow(ServerDnsIcon, "服务器", "连接地址、本机模式与登录", { model.navigate("server") })
        } }
        item { QimengSettingsGroup("使用偏好") {
            QimengSettingsRow(PaletteColorIcon, "外观", "${themeName(preferences.theme)} · ${preferences.material.label}", { model.navigate("appearance") })
            val preset = recommendation?.presets?.entries?.find { it.value == recommendation?.weights }?.key
            QimengSettingsRow(TuneSlidersIcon, "推荐偏好", if (recommendation == null) "正在读取…" else mapOf("balanced" to "均衡", "familiar" to "熟悉内容", "explore" to "探索", "fresh" to "新鲜内容")[preset] ?: "自定义偏好", { showPreferences = true })
            QimengSettingsRow(Icons.Outlined.Key, "AI 接入", "只读与写入权限、连接凭据和撤销", { model.navigate("ai-access") })
        } }
        item { QimengSettingsGroup("关于") { ServiceVersionRow(model) } }
        item { OutlinedButton(onClick = { confirmLogout = true }, enabled = !busy,
            colors = ButtonDefaults.outlinedButtonColors(contentColor = MaterialTheme.colorScheme.error),
            modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp)) { Text("退出登录") } }
    }
    if (confirmLogout) AlertDialog(onDismissRequest = { confirmLogout = false }, title = { Text("退出当前登录？") },
        text = { Text("退出后可重新连接服务器，未完成的传输记录会保留。") },
        confirmButton = { TextButton(onClick = { confirmLogout = false; model.logout() }, enabled = !busy) { Text("退出登录") } },
        dismissButton = { TextButton(onClick = { confirmLogout = false }) { Text("取消") } })
    if (showPreferences) ModalBottomSheet(onDismissRequest = { if (!busy) showPreferences = false }) {
        Box(Modifier.fillMaxWidth().fillMaxHeight(0.8f)) { RecommendationSettings(model) }
    }
}

@Composable
private fun ServiceVersionRow(model: LibraryViewModel) {
    val session by model.session.collectAsStateWithLifecycle()
    val revision by model.transportRevision.collectAsStateWithLifecycle()
    key(session, revision) { ServiceVersionRow(model, session, revision) }
}

@Composable
private fun ServiceVersionRow(model: LibraryViewModel, target: Session?, revision: Long) {
    var request by remember { mutableIntStateOf(0) }
    val state by produceState(LoadState<String>(loading = target != null), request) {
        if (target == null) return@produceState
        value = LoadState(loading = true)
        value = model.readServerVersion(target, revision)
    }
    val subtitle = when {
        target == null -> "未连接到服务"
        state.loading -> "正在读取服务版本…"
        state.error != null -> "${state.error} · 点击重试"
        state.data != null -> "服务端 ${state.data}"
        else -> "服务端未声明版本"
    }
    QimengSettingsRow(InfoCircleIcon, "版本信息", subtitle, if (state.error != null) ({ request++ }) else null)
}

/** Source: existing SettingsCards.CountCard; only the count type changes to the API's Long. */
@Composable
internal fun InventoryCountCard(title: String, count: Long?, modifier: Modifier = Modifier) {
    Surface(color = MaterialTheme.colorScheme.surfaceContainerLow, shape = RoundedCornerShape(16.dp),
        shadowElevation = if (isQimengDarkTheme()) 1.dp else 0.dp,
        modifier = modifier.heightIn(min = 88.dp).softDaylightShadow()) {
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.Center) {
            Text(title, style = MaterialTheme.typography.labelMedium.copy(fontSize = 13.sp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            Spacer(Modifier.height(2.dp))
            Text(count?.toString() ?: "—", style = MaterialTheme.typography.headlineMedium.copy(fontSize = 24.sp, fontWeight = FontWeight.Bold), color = MaterialTheme.colorScheme.onSurface)
        }
    }
}

@Composable
fun DataManageScreen(model: LibraryViewModel) {
    Column(Modifier.fillMaxSize()) {
    QimengTopBar("数据管理", onBack = model::back)
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).padding(bottom = 48.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        QimengPageIntro("从添加作品到整理和备份，照顾好你的媒体库。")
        QimengSettingsGroup("入库与整理") {
            QimengSettingsRow(CloudUploadIcon, "上传文件", "从系统文件中选择图片和视频", { model.navigate("upload") })
            QimengSettingsRow(FolderManagedIcon, "库管理", "注册目录、扫描内容与管理媒体库", { model.navigate("libraries") })
            QimengSettingsRow(Icons.Outlined.Assignment, "任务中心", "普通上传与智能整理的逐项结果", { model.navigate("reports") })
        }
        QimengSettingsGroup("词表与关联") {
            QimengSettingsRow(VocabularyBookIcon, "词表维护", "编辑原名提取词表、出处、角色与标签", { model.navigate("vocabulary-manage") })
        }
        QimengSettingsGroup("备份与存储") {
            QimengSettingsRow(BackupRestoreIcon, "资料备份与归档", "一个目录管理作者TXT、备份与上传归档", { model.navigate("maintenance") })
            QimengSettingsRow(StorageCacheIcon, "缩略图缓存", "查看生成进度，管理本机缓存空间", { model.navigate("thumbnail-cache") })
            QimengSettingsRow(Icons.Outlined.DeleteOutline, "回收站", "恢复移除的媒体", { model.navigate("trash") })
        }
    }
    }
}

@Composable
fun VocabularyManageScreen(model: LibraryViewModel) {
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp)) {
        QimengSettingsGroup("词表与关联") {
            QimengSettingsRow(VocabularyBookIcon, "原名提取词表", "维护提取用出处组、角色别名与停用词", { model.navigate("source-rules") })
            QimengSettingsRow(LabelTagIcon, "标签", "维护词表与媒体关联", { model.navigate("vocabulary:tags") })
            QimengSettingsRow(VocabularyBookIcon, "原作 / 出处", "维护名称与别名", { model.navigate("vocabulary:sources") })
            QimengSettingsRow(AuthorPeopleIcon, "角色", "维护角色及所属出处", { model.navigate("vocabulary:characters") })
        }
    }
}

@Composable
internal fun AuthorManagementTools(model: LibraryViewModel, item: Author) {
    val busy by model.busy.collectAsStateWithLifecycle()
    var edit by rememberSaveable(item.id) { mutableStateOf(false) }
    var deleting by rememberSaveable(item.id) { mutableStateOf(false) }
    var merging by rememberSaveable(item.id) { mutableStateOf(false) }
    if (item.kind == "cos") return
    QimengSettingsCard { Column(Modifier.padding(16.dp)) {
        Text("作者管理", style = MaterialTheme.typography.titleMedium)
        TextButton(onClick = { edit = true }, enabled = !busy) { Text("编辑名称与基本信息") }
        TextButton(onClick = { model.loadAuthors(); merging = true }, enabled = !busy) { Text("合并重复作者") }
        TextButton(onClick = { deleting = true }, enabled = !busy) { Text("删除作者") }
    } }
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

private fun themeName(theme: ThemeChoice): String = when (theme) { ThemeChoice.SYSTEM -> "跟随系统"; ThemeChoice.LIGHT -> "浅色"; ThemeChoice.DARK -> "深色" }
