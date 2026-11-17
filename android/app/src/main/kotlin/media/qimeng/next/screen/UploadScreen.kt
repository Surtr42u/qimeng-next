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
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { if (it.isNotEmpty()) model.receiveFiles(it) }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
        item { ContentCard {
            Text("上传位置", style = MaterialTheme.typography.titleMedium)
            if (libraries.isEmpty()) {
                Text("尚未读取到影库。请检查连接或在服务器创建影库。")
                TextButton(onClick = model::refreshConnection, enabled = !busy) { Text("刷新影库") }
            } else Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                libraries.forEach { library -> FilterChip(selected == library.id, { model.setUploadLibrary(library.id) }, label = { Text(library.name) }) }
            }
            OutlinedTextField(directory, model::setUploadDirectory, label = { Text("库内目录（可选）") }, singleLine = true, modifier = Modifier.fillMaxWidth())
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
            Button(onClick = model::startUpload, enabled = !busy && libraries.any { it.id == selected }, modifier = Modifier.fillMaxWidth()) { Text("开始上传 ${queue!!.drafts.size} 个文件") }
        }
        item { Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text("任务结果", Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
            TextButton(onClick = model::clearUploads, enabled = !busy) { Text("清理已结束记录") }
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
        if (queue?.events?.isNotEmpty() == true) item { Text("${queue!!.events.size} 条浏览事件等待补传", style = MaterialTheme.typography.bodySmall) }
        item { Spacer(Modifier.navigationBarsPadding()) }
    }
    queue?.drafts?.find { it.id == renameId }?.let { draft ->
        TextEditDialog("上传文件名", draft.name, busy, { renameId = null }) { model.renameDraft(draft.id, it); renameId = null }
    }
}

@Composable
fun TasksScreen(model: LibraryViewModel) {
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
    LaunchedEffect(id) { if (report?.id != id) model.openReport(id) }
    val task = report?.takeIf { it.id == id }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { TextButton(onClick = { model.openReport(id) }) { Text("刷新报告") } }
        if (task == null) item { Text("报告尚未读取") } else {
            item { ContentCard {
                Text(stateName(task.state), style = MaterialTheme.typography.headlineSmall)
                Text(task.summary)
                Text("来源文件始终保留。传输完成与智能整理完成分别显示。", style = MaterialTheme.typography.bodySmall)
            } }
            items(task.items, key = { it.id }) { item -> ContentCard {
                Text(item.fileName, style = MaterialTheme.typography.titleMedium)
                Text(stateName(item.state))
                if (item.message.isNotBlank()) Text(item.message)
                item.assetId?.let { asset -> TextButton(onClick = { model.openAsset(asset) }) { Text("查看入库结果") } }
            } }
        }
    }
}

private fun stateName(value: String) = mapOf("receiving" to "接收中", "waiting_executor" to "等待执行器", "ready" to "待处理", "partial" to "部分完成",
    "completed" to "已完成", "cancelled" to "已取消", "ingested" to "已入库", "staged" to "已传输，等待整理", "duplicate" to "重复，等待决定", "failed" to "失败")[value] ?: value
