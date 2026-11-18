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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
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
internal fun MaintenanceScreen(back: () -> Unit, upload: () -> Unit, archiveSettings: () -> Unit,
    device: @Composable () -> Unit, model: MaintenanceViewModel = viewModel()) {
    val state by model.state.collectAsStateWithLifecycle()
    var tab by rememberSaveable { mutableIntStateOf(0) }
    var details by rememberSaveable { mutableStateOf(false) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        val data = result.data
        if (result.resultCode == Activity.RESULT_OK && data != null) data.data?.let { model.bind(it, data.flags) }
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
                }
                state.folder.message?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary) }
                state.error?.let { ErrorNotice(it) }
            }
        }
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            listOf("备份恢复", "作者主表", "原件归档", "设备领取").forEachIndexed { index, name ->
                FilterChip(selected = tab == index, enabled = !state.busy, onClick = { tab = index },
                    modifier = Modifier.weight(1f).heightIn(min = 44.dp), shape = RoundedCornerShape(14.dp),
                    label = { Text(name, style = MaterialTheme.typography.labelLarge, maxLines = 1) })
            }
        }
        when (tab) {
            0 -> BackupMaintenance(state, model)
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
private fun BackupMaintenance(state: MaintenanceState, model: MaintenanceViewModel) {
    var document by remember(state.target) { mutableStateOf<Uri?>(null) }
    var source by rememberSaveable(state.target?.instanceId) { mutableStateOf("") }
    var accept by rememberSaveable(state.plan?.fingerprint) { mutableStateOf(false) }
    var confirming by remember { mutableStateOf(false) }
    var restoreInfo by remember { mutableStateOf(false) }
    val media = remember(state.document?.id) { mutableStateMapOf<String,String>() }
    val authors = remember(state.document?.id) { mutableStateMapOf<String,String>() }
    var edited by remember(state.plan?.fingerprint) { mutableStateOf(false) }
    val pick = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { file -> if (file != null) { model.resetPreview(); document = file; source = "" } }
    LazyColumn(Modifier.fillMaxSize().padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp), contentPadding = PaddingValues(bottom = 24.dp)) {
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
                OutlinedButton(onClick = { pick.launch(arrayOf("application/json", "text/plain", "application/octet-stream")) }, enabled = !state.busy,
                    modifier = Modifier.weight(1f), shape = RoundedCornerShape(12.dp)) { Text("导入恢复") }
            }
            TextButton(onClick = { restoreInfo = true }, enabled = state.folder.tree != null) { Icon(HistoryRecentIcon, null, Modifier.size(18.dp)); Text("恢复上次") }
            if (restoreInfo) Text("完整 .qnext 位于「上次 / 备份 / 影库备份.qnext」，相邻 .sha256 是完整性摘要。请用服务包的离线恢复命令还原到新的空目录。旧版 v2 JSON 可从上方导入合并。",
                style = MaterialTheme.typography.bodySmall)
        } }
        item { QimengSettingsCard { Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) { Text("自动备份", style = MaterialTheme.typography.titleSmall); Text("每天一次，打开影库时保存", style = MaterialTheme.typography.bodySmall) }
            Switch(state.folder.automatic, model::automatic, enabled = state.folder.tree != null && !state.busy)
        } } }
        if (document != null) item { MaintenanceCard(BackupRestoreIcon, "旧版 v2 导入") {
            Text("先同步媒体并完成扫描。请填写该份文档原安装的稳定标识。", style = MaterialTheme.typography.bodySmall)
            QimengCapsuleTextField(source, { source = it }, Modifier.fillMaxWidth(), placeholder = "来源安装标识", singleLine = true, enabled = state.document == null && !state.busy)
            Button(onClick = { model.preview(document, source, emptyMap(), emptyMap()) }, enabled = !state.busy && source.isNotBlank()) { Text("生成预检") }
        } }
        state.plan?.let { plan ->
            item { Text("匹配 ${plan.matched} / ${plan.media} · 跳过 ${plan.skipped} · 问题 ${plan.issues}") }
            items(state.mediaRows, key = { "media-${it.key}" }) { row -> MigrationMappingRow(row, media[row.key], plan.state == "prepared" && !state.busy) { media[row.key] = it; edited = true } }
            if (state.mediaCursor != null) item { TextButton(onClick = { model.nextRows(false) }, enabled = !state.busy) { Text("更多媒体") } }
            items(state.authorRows, key = { "author-${it.key}" }) { row -> MigrationMappingRow(row, authors[row.key], plan.state == "prepared" && !state.busy) { authors[row.key] = it; edited = true } }
            if (state.authorCursor != null) item { TextButton(onClick = { model.nextRows(true) }, enabled = !state.busy) { Text("更多作者") } }
            item { Column {
                if (plan.state == "prepared") {
                    if (edited) TextButton(onClick = { model.preview(null, "", media.toMap(), authors.toMap()) }, enabled = !state.busy) { Text("重新生成预检") }
                    if (plan.issues > 0 || plan.skipped > 0) Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(accept, { accept = it }); Text("已核对并接受报告中的跳过项和冲突")
                    }
                    Button(onClick = { confirming = true }, enabled = !edited && !state.busy && (accept || (plan.issues == 0 && plan.skipped == 0))) { Text("导入恢复") }
                } else Text("已按预检提交，导入前检查点 ${plan.checkpoint ?: "未返回"}")
            } }
        }
    }
    if (confirming && state.plan != null) AlertDialog(onDismissRequest = { confirming = false }, title = { Text("确认合并导入？") },
        text = { Text("将按已核对的计划合并资料和事件。重复导入沿原事件身份核对。") },
        confirmButton = { TextButton(onClick = { model.commit(state.plan, accept); confirming = false }) { Text("导入") } },
        dismissButton = { TextButton(onClick = { confirming = false }) { Text("取消") } })
}

@Composable
private fun MigrationMappingRow(row: MigrationRow, value: String?, editable: Boolean, change: (String) -> Unit) {
    QimengSettingsCard { Column(Modifier.padding(12.dp)) {
        Text(row.name, style = MaterialTheme.typography.titleSmall); Text((listOf(row.state, row.reason) + row.issues).filter(String::isNotBlank).joinToString(" · "), style = MaterialTheme.typography.bodySmall)
        if (editable) QimengCapsuleTextField(value ?: row.targetId.orEmpty(), change, Modifier.fillMaxWidth(), placeholder = "目标 ID", singleLine = true)
    } }
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
