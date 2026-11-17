package media.qimeng.next.screen

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.horizontalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.*
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice

@Composable
fun UploadScreen(model: LibraryViewModel) {
    val libraries by model.libraries.collectAsStateWithLifecycle()
    val selected by model.uploadLibrary.collectAsStateWithLifecycle()
    val directory by model.uploadDirectory.collectAsStateWithLifecycle()
    val mode by model.uploadMode.collectAsStateWithLifecycle()
    val queue by model.queue.collectAsStateWithLifecycle()
    val queueError by model.queueError.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val capabilities by model.capabilities.collectAsStateWithLifecycle()
    var renameId by rememberSaveable { mutableStateOf<String?>(null) }
    var chooseDirectory by rememberSaveable { mutableStateOf(false) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { if (it.isNotEmpty()) model.receiveFiles(it) }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
        item { ContentCard {
            Text("上传位置", style = MaterialTheme.typography.titleMedium)
            if (libraries.isEmpty()) {
                Text("尚未读取到影库。请检查连接或在服务器创建影库。")
                TextButton(onClick = model::refreshConnection, enabled = !busy) { Text("刷新影库") }
            } else Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                libraries.forEach { library -> FilterChip(selected == library.id, { model.setUploadLibrary(library.id) }, label = { Text(library.name) }, enabled = library.enabled && !busy) }
            }
            OutlinedTextField(directory, model::setUploadDirectory, label = { Text("库内目录（可选）") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            TextButton(onClick = { chooseDirectory = true }, enabled = libraries.any { it.id == selected && it.enabled }) { Text("浏览 / 创建目录") }
        } }
        item { ContentCard {
            Text("入库方式", style = MaterialTheme.typography.titleMedium)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(mode == UploadMode.ORDINARY.name, { model.setUploadMode(UploadMode.ORDINARY) }, label = { Text("普通上传") })
                FilterChip(mode == UploadMode.INTELLIGENT.name, { model.setUploadMode(UploadMode.INTELLIGENT) }, label = { Text("智能整理") })
            }
            Text(if (mode == UploadMode.ORDINARY.name) "按所选目录与名称入库，不依赖 AI。" else if (capabilities?.aiConfigured == false)
                "当前服务器尚未配置 AI 执行器。文件可传输并进入等待，任务中心会保留原因。" else "提交本批后开始智能整理，逐项结果会保存在任务中心。")
            Button(onClick = { picker.launch(arrayOf("image/*", "video/*")) }, enabled = !busy && queue != null) { Icon(Icons.Outlined.Add, null); Text("选择文件") }
            Text("文件选择后可预览名称再开始；来源原件保留。", style = MaterialTheme.typography.bodySmall)
        } }
        item { UploadAssociations(model) }
        queueError?.let { item { ErrorNotice(it, model::reloadQueue) } }
        items(queue?.drafts.orEmpty(), key = { "draft:${it.id}" }) { draft ->
            ContentCard {
                Text(draft.name, style = MaterialTheme.typography.titleMedium)
                Text("${draft.mimeType} · ${formatBytes(draft.bytes)}", style = MaterialTheme.typography.bodySmall)
                Row {
                    TextButton(onClick = { renameId = draft.id }, enabled = !busy) { Text("改名") }
                    TextButton(onClick = { model.removeDraft(draft.id) }, enabled = !busy) { Text("移除待传项") }
                }
            }
        }
        if (queue?.drafts?.isNotEmpty() == true) item {
            Button(onClick = model::startUpload, enabled = !busy && libraries.any { it.id == selected && it.enabled }, modifier = Modifier.fillMaxWidth()) { Text("开始上传 ${queue!!.drafts.size} 个文件") }
        }
        item { Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text("任务结果", Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
            TextButton(onClick = model::clearUploads, enabled = !busy) { Text("清理已结束记录") }
        } }
        items(queue?.intakeFailures.orEmpty(), key = { "rejected:${it.id}" }) { rejected -> ContentCard {
            Text(rejected.name, style = MaterialTheme.typography.titleMedium)
            ErrorNotice(rejected.message)
            Text("来源文件保留，修复后可重新选择。", style = MaterialTheme.typography.bodySmall)
        } }
        items(queue?.transfers.orEmpty().asReversed(), key = { "transfer:${it.id}" }) { transfer ->
            ContentCard {
                Text(transfer.name, style = MaterialTheme.typography.titleMedium)
                Text(transfer.message)
                Text("目标：${transfer.target.label}", style = MaterialTheme.typography.bodySmall)
                if (transfer.state == TransferState.UPLOADING || transfer.state == TransferState.QUEUED) {
                    LinearProgressIndicator(progress = { if (transfer.bytes == 0L) 0f else (transfer.offset.toDouble() / transfer.bytes).toFloat() }, modifier = Modifier.fillMaxWidth())
                    Text("${formatBytes(transfer.offset)} / ${formatBytes(transfer.bytes)}", style = MaterialTheme.typography.bodySmall)
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (canRetry(transfer.state)) TextButton(onClick = { model.retryUpload(transfer.id) }, enabled = !busy) { Text("重试") }
                    if (transfer.state !in setOf(TransferState.SUCCEEDED, TransferState.CANCELLED)) TextButton(onClick = { model.cancelUpload(transfer.id) }, enabled = !busy) { Text("停止") }
                    transfer.batchId?.let { batch -> TextButton(onClick = { model.openReport(batch) }) { Text("报告") } }
                    transfer.assetId?.let { id -> TextButton(onClick = { model.openAsset(id) }) { Text("查看媒体") } }
                }
            }
        }
        if (queue?.events?.isNotEmpty() == true) item {
            ContentCard {
                Text("${queue!!.events.size} 条浏览事件等待补传", style = MaterialTheme.typography.bodySmall)
                queue!!.events.firstOrNull { it.failure != null }?.failure?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                TextButton(onClick = model::retryEvents) { Text("重试浏览记录补传") }
            }
        }
        item { Spacer(Modifier.navigationBarsPadding()) }
    }
    queue?.drafts?.find { it.id == renameId }?.let { draft ->
        TextEditDialog("上传文件名", draft.name, busy, { renameId = null }) { model.renameDraft(draft.id, it) { renameId = null } }
    }
    if (chooseDirectory) DirectoryDialog(model, selected, directory) { chooseDirectory = false }
}

@Composable
private fun UploadAssociations(model: LibraryViewModel) {
    var expanded by rememberSaveable { mutableStateOf(false) }
    var chooseAuthors by rememberSaveable { mutableStateOf(false) }
    var chooseCollection by rememberSaveable { mutableStateOf(false) }
    var newCollection by rememberSaveable { mutableStateOf(false) }
    val authorIds by model.uploadAuthorIds.collectAsStateWithLifecycle()
    val collectionId by model.uploadCollectionId.collectAsStateWithLifecycle()
    val note by model.uploadNote.collectAsStateWithLifecycle()
    val acquisitionSources by model.uploadSources.collectAsStateWithLifecycle()
    val libraryId by model.uploadLibrary.collectAsStateWithLifecycle()
    val libraries by model.libraries.collectAsStateWithLifecycle()
    val authors by model.authors.collectAsStateWithLifecycle()
    val collections by model.collections.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val libraryKind = libraries.find { it.id == libraryId }?.kind
    ContentCard {
        TextButton(onClick = { expanded = !expanded }) { Text("可选关联 · ${authorIds.size} 位作者${if (collectionId == null) "" else " · 已选集合"}") }
        if (expanded) {
            Text("仅对这次提交的全部待传文件应用所选关联；混合内容可保持未指定。", style = MaterialTheme.typography.bodySmall)
            OutlinedButton(onClick = { model.loadAuthors(); chooseAuthors = true }, enabled = !busy) { Text("选择作者") }
            OutlinedTextField(acquisitionSources, model::setUploadSources, label = { Text("获取渠道 / 来源（每行一条）") }, modifier = Modifier.fillMaxWidth())
            OutlinedButton(onClick = { model.loadCollections(); chooseCollection = true }, enabled = libraryId.isNotEmpty() && !busy) { Text("组图 / 短剧集合") }
            OutlinedTextField(note, model::setUploadNote, label = { Text("本批备注（原样保存）") }, minLines = 2, modifier = Modifier.fillMaxWidth())
        }
    }
    if (chooseAuthors) {
        var selected by rememberSaveable { mutableStateOf(authorIds) }
        AlertDialog(onDismissRequest = { chooseAuthors = false }, title = { Text("本批关联作者") }, text = {
            LazyColumn(Modifier.heightIn(max = 360.dp)) {
                items(authors.items.filter { libraryKind == null || it.kind == libraryKind }, key = { it.id }) { author -> Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(author.id in selected, { checked -> selected = if (checked) selected + author.id else selected - author.id }); Text(author.name)
                } }
                if (authors.cursor != null) item { TextButton(onClick = { model.loadAuthors(true) }, enabled = !authors.loading) { Text("更多作者") } }
                authors.error?.let { item { ErrorNotice(it) { model.loadAuthors() } } }
            }
        }, confirmButton = { TextButton(onClick = { model.setUploadAuthors(selected); chooseAuthors = false }) { Text("应用") } },
            dismissButton = { TextButton(onClick = { chooseAuthors = false }) { Text("取消") } })
    }
    if (chooseCollection) AlertDialog(onDismissRequest = { chooseCollection = false }, title = { Text("组图 / 短剧集合") }, text = {
        LazyColumn(Modifier.heightIn(max = 360.dp)) {
            item { TextButton(onClick = { model.setUploadCollection(null); chooseCollection = false }) { Text("不关联集合") } }
            items(collections.items, key = { it.id }) { collection -> TextButton(onClick = { model.setUploadCollection(collection.id); chooseCollection = false }) {
                Text("${collection.name} · ${if (collection.kind == "short_drama") "短剧" else "组图"}")
            } }
            if (collections.cursor != null) item { TextButton(onClick = { model.loadCollections(true) }) { Text("更多集合") } }
            collections.error?.let { item { ErrorNotice(it) { model.loadCollections() } } }
        }
    }, confirmButton = { TextButton(onClick = { newCollection = true }) { Text("新建集合") } },
        dismissButton = { TextButton(onClick = { chooseCollection = false }) { Text("关闭") } })
    if (newCollection) {
        var name by rememberSaveable { mutableStateOf("") }
        var kind by rememberSaveable { mutableStateOf("image_set") }
        AlertDialog(onDismissRequest = { if (!busy) newCollection = false }, title = { Text("新建集合") }, text = {
            Column {
                OutlinedTextField(name, { name = it }, label = { Text("名称") })
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilterChip(kind == "image_set", { kind = "image_set" }, label = { Text("组图") })
                    FilterChip(kind == "short_drama", { kind = "short_drama" }, label = { Text("短剧") })
                }
            }
        }, confirmButton = { TextButton(onClick = { model.createCollection(name.trim(), kind) { newCollection = false } }, enabled = !busy && name.isNotBlank()) { Text("创建") } },
            dismissButton = { TextButton(onClick = { newCollection = false }, enabled = !busy) { Text("取消") } })
    }
}

@Composable
private fun DirectoryDialog(model: LibraryViewModel, libraryId: String, initial: String, close: () -> Unit) {
    var parent by rememberSaveable(libraryId) { mutableStateOf(initial) }
    var create by rememberSaveable { mutableStateOf(false) }
    val state by model.directories.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    LaunchedEffect(libraryId, parent) { model.loadDirectories(libraryId, parent) }
    AlertDialog(onDismissRequest = close, title = { Text("选择库内目录") }, text = {
        Column(Modifier.heightIn(max = 420.dp)) {
            Text(parent.ifEmpty { "影库根目录" })
            Row {
                TextButton(onClick = { parent = parent.substringBeforeLast('/', "") }, enabled = parent.isNotEmpty()) { Text("上一级") }
                TextButton(onClick = { create = true }, enabled = !busy) { Text("新建目录") }
            }
            if (state.loading) LinearProgressIndicator(Modifier.fillMaxWidth())
            state.error?.let { ErrorNotice(it) { model.loadDirectories(libraryId, parent) } }
            LazyColumn(Modifier.weight(1f, fill = false)) {
                items(state.items, key = { it.path }) { directory -> TextButton(onClick = { parent = directory.path }) { Icon(Icons.Outlined.Folder, null); Text(directory.name) } }
                if (state.cursor != null) item { TextButton(onClick = { model.loadDirectories(libraryId, parent, true) }, enabled = !state.loading) { Text("加载更多目录") } }
            }
        }
    }, confirmButton = { TextButton(onClick = { model.setUploadDirectory(parent); close() }, enabled = state.loaded) { Text("选择当前目录") } },
        dismissButton = { TextButton(onClick = close) { Text("取消") } })
    if (create) TextEditDialog("新建目录名称", "", busy, { create = false }) { model.createDirectory(libraryId, parent, it) { create = false } }
}

@Composable
fun TasksScreen(model: LibraryViewModel) {
    var authors by rememberSaveable { mutableStateOf(false) }
    Column(Modifier.fillMaxSize()) {
        Row(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(!authors, { authors = false }, label = { Text("入库 / 整理") })
            FilterChip(authors, { authors = true }, label = { Text("作者资料") })
        }
        Box(Modifier.weight(1f)) { if (authors) AuthorTasksScreen(model) else BatchTasksScreen(model) }
    }
}

@Composable
private fun BatchTasksScreen(model: LibraryViewModel) {
    val state by model.reports.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (!state.loaded) model.loadReports() }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { TextButton(onClick = { model.loadReports() }) { Text("刷新任务") } }
        state.error?.let { item { ErrorNotice(it) { model.loadReports() } } }
        if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
        if (state.loaded && state.items.isEmpty()) item { Text("暂无任务报告") }
        items(state.items, key = { it.id }) { task -> ContentCard {
            Text(task.summary, style = MaterialTheme.typography.titleMedium)
            Text("${stateName(task.state)} · ${task.createdAt}")
            TextButton(onClick = { model.openReport(task.id) }) { Text("逐项结果") }
        } }
        if (state.cursor != null) item { TextButton(onClick = { model.loadReports(true) }, enabled = !state.loading) { Text("加载更多任务") } }
    }
}

@Composable
fun TaskDetailScreen(model: LibraryViewModel, id: String) {
    val report by model.report.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    var cancelling by rememberSaveable(id) { mutableStateOf(false) }
    LaunchedEffect(id) { if (report?.id != id) model.openReport(id) }
    val task = report?.takeIf { it.id == id }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { TextButton(onClick = { model.openReport(id) }) { Text("刷新报告") } }
        if (task == null) item { Text("报告尚未读取") } else {
            item { ContentCard {
                Text(stateName(task.state), style = MaterialTheme.typography.headlineSmall)
                Text(task.summary)
                Text("来源文件始终保留。传输完成与智能整理完成分别显示。", style = MaterialTheme.typography.bodySmall)
                if (task.state !in setOf("completed", "cancelled")) TextButton(onClick = { cancelling = true }, enabled = !busy) { Text("取消尚未执行的任务") }
            } }
            items(task.items, key = { it.id }) { item -> ContentCard {
                Text(item.fileName, style = MaterialTheme.typography.titleMedium)
                Text(stateName(item.state))
                if (item.message.isNotBlank()) Text(item.message)
                item.assetId?.let { asset -> TextButton(onClick = { model.openAsset(asset) }) { Text("查看入库结果") } }
            } }
        }
    }
    if (cancelling) AlertDialog(onDismissRequest = { cancelling = false }, title = { Text("停止后续整理？") },
        text = { Text("尚未执行的任务会停止；已在服务器入库的媒体和结果保留。") },
        confirmButton = { TextButton(onClick = { model.cancelReport(); cancelling = false }, enabled = !busy) { Text("停止后续任务") } },
        dismissButton = { TextButton(onClick = { cancelling = false }) { Text("取消") } })
}

private fun stateName(value: String) = mapOf("receiving" to "接收中", "waiting_executor" to "等待执行器", "ready" to "待处理", "partial" to "部分完成",
    "completed" to "已完成", "cancelled" to "已取消", "ingested" to "已入库", "staged" to "已传输，等待整理", "duplicate" to "重复，等待决定", "failed" to "失败")[value] ?: value
