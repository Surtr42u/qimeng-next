package media.qimeng.next.screen

import android.app.Activity
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.MaintenanceState
import media.qimeng.next.MaintenanceViewModel
import media.qimeng.next.model.*
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.*
import media.qimeng.next.ui.icon.*
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.UUID

@Composable
internal fun MaintenanceScreen(library: LibraryViewModel, back: () -> Unit, upload: () -> Unit, archiveSettings: () -> Unit,
    device: @Composable () -> Unit, model: MaintenanceViewModel = viewModel()) {
    val state by model.state.collectAsStateWithLifecycle()
    var tab by rememberSaveable { mutableIntStateOf(0) }
    var details by rememberSaveable { mutableStateOf(false) }
    var pendingPick by remember { mutableStateOf<Pair<Uri, Int>?>(null) }
    var abandoning by remember { mutableStateOf(false) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        val data = result.data
        if (result.resultCode == Activity.RESULT_OK && data != null) data.data?.let { uri ->
            if (state.folder.pending && state.folder.tree != uri.toString()) pendingPick = uri to data.flags
            else model.bind(uri, data.flags)
        }
    }
    val choose = { picker.launch(Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or
        Intent.FLAG_GRANT_WRITE_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION or Intent.FLAG_GRANT_PREFIX_URI_PERMISSION)) }
    Column(Modifier.fillMaxSize()) {
        QimengTopBar("资料维护", onBack = back)
        QimengSettingsCard(Modifier.padding(horizontal = 16.dp)) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    Surface(shape = RoundedCornerShape(12.dp), color = MaterialTheme.colorScheme.surfaceVariant) {
                        Icon(FolderManagedIcon, null, Modifier.padding(10.dp).size(24.dp))
                    }
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                        Text(state.folder.label.ifBlank { "集中管理你的资料" }, style = MaterialTheme.typography.titleSmall)
                        Text(if (state.folder.tree == null) "一个目录，保存备份、作者与归档" else "维护目录已连接",
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    if (state.folder.tree != null) IconButton(enabled = !state.busy, onClick = { details = !details }) { Icon(TuneSlidersIcon, "目录选项") }
                }
                if (state.folder.tree == null) Button(onClick = choose, enabled = !state.busy, shape = RoundedCornerShape(12.dp)) { Text("选择维护目录") }
                if (details && state.folder.tree != null) Row {
                    TextButton(onClick = choose, enabled = !state.busy) { Text("更换目录") }
                    TextButton(onClick = model::export, enabled = !state.busy) { Text("刷新目录") }
                    if (state.folder.pending) TextButton(onClick = { abandoning = true }, enabled = !state.busy) { Text("放弃轮换") }
                }
                if (state.folder.pending) Text("上次轮换未完成：可继续导出，或放弃后更换目录。旧的两代资料与上传归档保留。",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.tertiary)
                state.folder.message?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary) }
                state.error?.let { ErrorNotice(it) }
            }
        }
        BoxWithConstraints(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
            val labels = listOf("备份恢复", "作者主表", "原件归档", "设备领取")
            val style = MaterialTheme.typography.labelLarge
            val chipWidth = with(LocalDensity.current) { style.fontSize.toDp() } * 4 + 32.dp
            val columns = if (maxWidth >= chipWidth * 4 + 24.dp) 4 else 2
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                labels.indices.chunked(columns).forEach { indices ->
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        indices.forEach { index ->
                            FilterChip(selected = tab == index, enabled = !state.busy, onClick = { tab = index },
                                modifier = Modifier.weight(1f).heightIn(min = 44.dp), shape = RoundedCornerShape(14.dp),
                                label = { Text(labels[index], style = style) })
                        }
                    }
                }
            }
        }
        when (tab) {
            0 -> BackupMaintenance(state, model, library)
            1 -> AuthorMaintenance(state, model)
            2 -> Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp)) {
                MaintenanceCard(ArchiveBoxIcon, "上传后的原件") {
                    Text("归档来源由维护目录保存，可按媒体库重新上传。", style = MaterialTheme.typography.bodySmall)
                    Surface(shape = RoundedCornerShape(12.dp), color = MaterialTheme.colorScheme.surfaceVariant) {
                        Column(Modifier.fillMaxWidth().padding(12.dp)) {
                            Text("保存位置", style = MaterialTheme.typography.labelMedium)
                            Text(if (state.folder.tree != null) "维护目录 / 上传归档" else "当前归档目录", style = MaterialTheme.typography.bodyMedium)
                        }
                    }
                    Button(onClick = upload, modifier = Modifier.fillMaxWidth(), shape = RoundedCornerShape(12.dp)) {
                        Icon(CloudUploadIcon, null, Modifier.padding(end = 8.dp).size(18.dp)); Text("前往上传文件")
                    }
                    TextButton(onClick = archiveSettings) { Text("查看归档文件夹") }
                }
            }
            else -> device()
        }
    }
    pendingPick?.let { (uri, flags) ->
        AlertDialog(onDismissRequest = { pendingPick = null }, title = { Text("上次轮换未完成") },
            text = { Text("当前维护目录还有一份未完成的备份轮换。继续完成需要等待服务器导出；也可以放弃这次轮换并更换目录。已有的「当前 / 上次」资料和上传归档不会被删除。") },
            confirmButton = { TextButton(onClick = { model.replaceDirectory(uri, flags); pendingPick = null }, enabled = !state.busy) { Text("放弃并更换") } },
            dismissButton = { Row {
                TextButton(onClick = { model.export(); pendingPick = null }, enabled = !state.busy) { Text("继续完成") }
                TextButton(onClick = { pendingPick = null }) { Text("取消") }
            } })
    }
    if (abandoning) AlertDialog(onDismissRequest = { abandoning = false }, title = { Text("放弃未完成的轮换？") },
        text = { Text("只清理本次未完成的暂存文件和轮换标记。已有的「当前 / 上次」资料、上传归档和服务器检查点保留，下次导出会重新开始。") },
        confirmButton = { TextButton(onClick = { model.abandonRotation(); abandoning = false }, enabled = !state.busy) { Text("放弃轮换") } },
        dismissButton = { TextButton(onClick = { abandoning = false }) { Text("取消") } })
}

@Composable
internal fun MaintenanceCard(icon: ImageVector, title: String, content: @Composable ColumnScope.() -> Unit) {
    QimengSettingsCard {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Icon(icon, null, Modifier.size(24.dp)); Text(title, style = MaterialTheme.typography.titleMedium)
            }
            content()
        }
    }
}

@Composable
private fun BackupMaintenance(state: MaintenanceState, model: MaintenanceViewModel, library: LibraryViewModel) {
    var restoreInfo by remember { mutableStateOf(false) }
    LazyColumn(Modifier.fillMaxSize().padding(horizontal = 16.dp).testTag("maintenance-backup-list"),
        verticalArrangement = Arrangement.spacedBy(12.dp), contentPadding = PaddingValues(bottom = 24.dp)) {
        item { MaintenanceCard(BackupRestoreIcon, "备份与恢复") {
            Text("保存作品原件、作者、头像、标签及浏览记录。", style = MaterialTheme.typography.bodySmall)
            Surface(shape = RoundedCornerShape(12.dp), color = MaterialTheme.colorScheme.surfaceVariant) {
                Column(Modifier.fillMaxWidth().padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text("最新备份 · 自动保留当前/上次两代", style = MaterialTheme.typography.labelMedium)
                    Text(if (state.folder.tree == null) "先设置维护目录" else if (state.folder.lastChecked == 0L) "还没有保存备份"
                        else DateTimeFormatter.ofPattern("MM-dd HH:mm").withZone(ZoneId.systemDefault()).format(Instant.ofEpochMilli(state.folder.lastChecked)))
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Button(onClick = model::export, enabled = state.folder.tree != null && !state.busy, modifier = Modifier.weight(1f), shape = RoundedCornerShape(12.dp)) { Text("导出备份") }
                OutlinedButton(onClick = { restoreInfo = !restoreInfo }, enabled = !state.busy,
                    modifier = Modifier.weight(1f), shape = RoundedCornerShape(12.dp)) { Text("恢复说明") }
            }
            TextButton(onClick = { restoreInfo = true }, enabled = state.folder.tree != null) { Icon(HistoryRecentIcon, null, Modifier.size(18.dp)); Text("恢复上次") }
            if (restoreInfo) {
                Text("每代备份中的「备份 / 影库备份.qnext」配有独立 .sha256 摘要。请用配套离线恢复命令还原到新的空目录。PC 或 NAS 选定的资料可通过「设备领取」接收。", style = MaterialTheme.typography.bodySmall)
            }
        } }
        item { QimengSettingsCard { Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) { Text("自动备份", style = MaterialTheme.typography.titleSmall); Text("每天一次，打开影库时保存", style = MaterialTheme.typography.bodySmall) }
            Switch(state.folder.automatic, model::automatic, enabled = state.folder.tree != null && !state.busy)
        } } }
        item { ActivityEventCard(library) }
    }
}

@Composable
private fun AuthorMaintenance(state: MaintenanceState, model: MaintenanceViewModel) {
    var advanced by rememberSaveable { mutableStateOf(false) }
    var removing by remember { mutableStateOf<AuthorFragment?>(null) }
    val pick = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { it?.let { uri -> model.prepareAuthor(uri) } }
    LazyColumn(Modifier.fillMaxSize().padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp), contentPadding = PaddingValues(bottom = 24.dp)) {
        item { MaintenanceCard(TxtImportFileIcon, "作者主表") {
            Text("主表记录作者、来源与作品关联。简介和头像随完整备份保存。", style = MaterialTheme.typography.bodySmall)
            if (state.folder.tree != null) Button(onClick = { model.prepareAuthor() }, enabled = !state.busy, modifier = Modifier.fillMaxWidth(), shape = RoundedCornerShape(12.dp)) { Text("导入 作者总表.txt") }
            OutlinedButton(onClick = { pick.launch(arrayOf("text/plain", "application/octet-stream")) }, enabled = !state.busy, modifier = Modifier.fillMaxWidth()) { Text("选择其他 TXT") }
        } }
        state.summary?.let { summary -> item { Text("新建作者 ${summary.newAuthors} · 匹配作品 ${summary.matched} · 更新关联 ${summary.changed}") }
            items(summary.issues) { Text(it, style = MaterialTheme.typography.bodySmall) } }
        item { TextButton(onClick = { advanced = !advanced }) { Text(if (advanced) "收起来源与高级维护" else "来源与高级维护") } }
        if (advanced) {
            item { Text("同名片段按修订覆盖。移除片段后按剩余来源重新匹配，作者资料保留。", style = MaterialTheme.typography.bodySmall)
                TextButton(onClick = model::rebuild, enabled = !state.busy && state.fragments.isNotEmpty()) { Text("重新匹配") } }
            items(state.fragments, key = AuthorFragment::id) { row -> QimengSettingsCard { Row(Modifier.padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(row.name, Modifier.weight(1f)); TextButton(onClick = { removing = row }, enabled = !state.busy) { Text("移除") }
            } } }
            if (state.cursor != null) item { TextButton(onClick = { model.refreshAuthors(true) }, enabled = !state.busy) { Text("更多片段") } }
            state.mirror?.let { initial -> item { AuthorMirrorCard(initial, state, model) } }
        }
    }
    val draft = state.draft
    val missing = state.missing
    if (draft != null) AlertDialog(onDismissRequest = model::dismissDraft, title = { Text(if (missing == null) "导入作者 TXT？" else "重新导入会移除上传挂靠条目") },
        text = { Column(Modifier.heightIn(max = 340.dp).verticalScroll(rememberScrollState())) {
            Text(draft.name)
            if (missing == null) Text("将按已保存片段重新计算常规作者与作品关联。")
            else { Text("待决定 ${missing.total} 项"); missing.examples.forEach { Text("${it.first} · ${it.second}") } }
            state.error?.let { ErrorNotice(it) }
        } }, confirmButton = { Row {
            if (missing != null) TextButton(onClick = { model.importAuthor("remove") }, enabled = !state.busy) { Text("确认移除", color = MaterialTheme.colorScheme.error) }
            TextButton(onClick = { model.importAuthor(if (missing == null) null else "keep") }, enabled = !state.busy) { Text(if (missing == null) "导入" else "保留并导入") }
        } }, dismissButton = { TextButton(onClick = model::dismissDraft, enabled = !state.busy) { Text("取消") } })
    removing?.let { item -> AlertDialog(onDismissRequest = { removing = null }, title = { Text("移除这份来源？") }, text = { Text("${item.name}\n将按剩余来源重新计算作品关联，作者资料保留。") },
        confirmButton = { TextButton(onClick = { model.remove(item); removing = null }) { Text("移除") } }, dismissButton = { TextButton(onClick = { removing = null }) { Text("取消") } }) }
}

@Composable
private fun AuthorMirrorCard(initial: AuthorMirror, state: MaintenanceState, model: MaintenanceViewModel) {
    var path by rememberSaveable(initial.revision) { mutableStateOf(initial.path) }
    var fragment by rememberSaveable(initial.revision) { mutableStateOf(initial.mode == "fragment") }
    var id by rememberSaveable(initial.revision) { mutableStateOf(initial.fragmentId) }
    val key = remember(initial.revision, path, fragment, id) { UUID.randomUUID().toString() }
    MaintenanceCard(TxtImportFileIcon, "作者总表镜像") {
        Text("镜像文件路径（服务器授权存储中的绝对 TXT 路径，留空关闭）", style = MaterialTheme.typography.labelMedium)
        QimengCapsuleTextField(path, { path = it }, Modifier.fillMaxWidth(), singleLine = true)
        Row { FilterChip(!fragment, { fragment = false }, label = { Text("总表") }); FilterChip(fragment, { fragment = true }, label = { Text("来源片段") }) }
        if (fragment) {
            FilterChip(id == null, { id = null }, label = { Text("最近导入片段") })
            state.fragments.forEach { row -> FilterChip(id == row.id, { id = row.id }, label = { Text(row.name) }) }
            if (state.cursor != null) TextButton(onClick = { model.refreshAuthors(true) }) { Text("更多片段") }
        }
        Text(when (initial.state) { "disabled" -> "镜像已关闭"; "current" -> "已写入"; "pending" -> "等待写入"; else -> "镜像写入失败 ${initial.failure.orEmpty()}" }, style = MaterialTheme.typography.bodySmall)
        Row { TextButton(onClick = { model.refreshAuthors() }, enabled = !state.busy) { Text("刷新状态") }
            Button(onClick = { model.mirror(initial, path, fragment, id, key) }, enabled = !state.busy) { Text("保存镜像配置") } }
    }
}
