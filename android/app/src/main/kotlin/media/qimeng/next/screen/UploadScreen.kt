package media.qimeng.next.screen

import android.webkit.MimeTypeMap
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.clickable
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
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.NamingViewModel
import media.qimeng.next.model.*
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.*

@Composable
fun UploadScreen(model: LibraryViewModel, naming: NamingViewModel = viewModel()) {
    val libraries by model.libraries.collectAsStateWithLifecycle()
    val selected by model.uploadLibrary.collectAsStateWithLifecycle()
    val directory by model.uploadDirectory.collectAsStateWithLifecycle()
    val mode by model.uploadMode.collectAsStateWithLifecycle()
    val queue by model.queue.collectAsStateWithLifecycle()
    val session by model.session.collectAsStateWithLifecycle()
    val transport by model.transportRevision.collectAsStateWithLifecycle()
    val queueError by model.queueError.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val capabilities by model.capabilities.collectAsStateWithLifecycle()
    val autoName by model.uploadAutoName.collectAsStateWithLifecycle()
    val family by model.uploadFamily.collectAsStateWithLifecycle()
    val collectionId by model.uploadCollectionId.collectAsStateWithLifecycle()
    val note by model.uploadNote.collectAsStateWithLifecycle()
    val namingState by naming.state.collectAsStateWithLifecycle()
    val stems by model.editedStems.collectAsStateWithLifecycle()
    val drafts = queue?.drafts.orEmpty().filter { it.intakeGroup == null }
    var numberClass by rememberSaveable { mutableStateOf("image") }
    var activeName by rememberSaveable { mutableStateOf<String?>(null) }
    val namingDrafts = drafts.filter { it.bytes > 0 }
    val needsNaming = mode == UploadMode.ORDINARY.name && (autoName || collectionId != null) && namingDrafts.isNotEmpty()
    val namingRequest = if (needsNaming && selected.isNotEmpty()) NamingRequest(selected, directory, family.trim(), collectionId, numberClass, note,
        namingDrafts.map { NamingFile(it.id, it.withEditedStem(stems[it.id]).name) }) else null
    LaunchedEffect(namingRequest) { naming.inspect(namingRequest) }
    var showGuide by rememberSaveable { mutableStateOf(false) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { if (it.isNotEmpty()) model.receiveFiles(it) }
    LaunchedEffect(Unit) { model.loadLibraries() }
    Column(Modifier.fillMaxSize()) {
        QimengTopBar("上传", onBack = model::back)
        LazyColumn(Modifier.fillMaxSize().imePadding().testTag("upload-list"), contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { QimengPageIntro("选好上传位置，再添加你想收藏的作品。") }
        item { UploadDestinationSection(model) }
        item { session?.let { target -> key(target, transport) { UploadAssociations(model, target, transport) } } }
        item { UploadPanel("添加文件", "选择后可修改文件名，确认后点「开始上传」。作者与来源可稍后整理。") {
            Button(onClick = { picker.launch(arrayOf("image/*", "video/*")) }, enabled = !busy && queue != null, modifier = Modifier.fillMaxWidth()) { Text("系统文件") }
            TextButton(onClick = { showGuide = !showGuide }) { Text(if (showGuide) "收起选择说明" else "权限与格式说明") }
            if (showGuide) Text("「系统文件」用系统文档选择器：可直接选到点前缀隐藏目录里的媒体，无需「所有文件访问」授权。支持的图片和视频格式由服务器核验。",
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        } }
        queueError?.let { item { ErrorNotice(it, model::reloadQueue) } }
        if (drafts.isNotEmpty()) item {
            QimengSettingsCard {
                Column(Modifier.fillMaxWidth().padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("待上传文件（${drafts.size} 项）", style = MaterialTheme.typography.titleSmall)
                    drafts.forEach { draft -> key(draft.id) {
                        UploadFileRow(draft, stems[draft.id], busy, activeName == draft.id && mode == UploadMode.ORDINARY.name,
                            session, transport, selected, directory, { activeName = draft.id }, { model.editStem(draft, it) },
                            { model.removeDraft(draft.id) })
                    } }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        QimengSegPill("普通上传", mode == UploadMode.ORDINARY.name, { model.setUploadMode(UploadMode.ORDINARY) }, enabled = !busy)
                        QimengSegPill("智能整理", mode == UploadMode.INTELLIGENT.name, { model.setUploadMode(UploadMode.INTELLIGENT) }, enabled = !busy)
                    }
                    if (mode == UploadMode.INTELLIGENT.name && capabilities?.aiConfigured == false)
                        Text("尚未配置 AI 执行器。服务器接收后会保留等待原因。", style = MaterialTheme.typography.bodySmall)
                    if (mode == UploadMode.ORDINARY.name) UploadNamingCard(autoName, family, collectionId, numberClass, namingRequest, namingState, busy,
                        model::setUploadAutoName, model::setUploadFamily, { numberClass = it }, { naming.inspect(namingRequest) })
                    Button(onClick = model::startUpload, enabled = !busy && libraries.any { it.id == selected && it.enabled } &&
                        drafts.all { draft -> stems[draft.id]?.let(draft::validStem) != false } &&
                        (!needsNaming || (namingState.inspectionRequest == namingRequest && !namingState.loading && namingState.error == null && namingState.inspection?.canSubmit == true)),
                        modifier = Modifier.fillMaxWidth()) { Text(if (busy) "上传中…" else "开始上传（${drafts.size} 项）") }
                }
            }
        }
        item { ArchiveUploadSection({ model.navigate("archive-folder") }) }
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
            val onTarget = sameTarget(transfer.target, session)
            ContentCard(Modifier.testTag("transfer:${transfer.id}")) {
                Text(transfer.name, style = MaterialTheme.typography.titleMedium)
                Text(transfer.message)
                Text("目标：${transfer.target.label}", style = MaterialTheme.typography.bodySmall)
                if (!onTarget && (transfer.batchId != null || transfer.assetId != null)) Text("连接此任务原来的服务器后可查看结果", style = MaterialTheme.typography.bodySmall)
                if (transfer.state == TransferState.UPLOADING || transfer.state == TransferState.QUEUED) {
                    LinearProgressIndicator(progress = { if (transfer.bytes == 0L) 0f else (transfer.offset.toDouble() / transfer.bytes).toFloat() }, modifier = Modifier.fillMaxWidth())
                    Text("${formatBytes(transfer.offset)} / ${formatBytes(transfer.bytes)}", style = MaterialTheme.typography.bodySmall)
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (canRetry(transfer.state)) TextButton(onClick = { model.retryUpload(transfer.id) }, enabled = !busy) { Text("重试") }
                    if (transfer.archiveSourceState == "blocked") TextButton(onClick = { model.retryArchiveSource(transfer) }, enabled = !busy) { Text("重新核对备份和来源") }
                    if (!transfer.state.isFinished()) TextButton(onClick = { model.cancelUpload(transfer.id) }, enabled = !busy) { Text("停止") }
                    transfer.batchId?.let { batch -> TextButton(onClick = { model.openReport(batch, transfer.target) }, enabled = onTarget) { Text("报告") } }
                    transfer.assetId?.let { id -> TextButton(onClick = { model.openAsset(id, target = transfer.target) }, enabled = onTarget) { Text("查看媒体") } }
                }
            }
        }
        if (queue?.events?.isNotEmpty() == true) item { ActivityEventCard(model) }
        item { Spacer(Modifier.navigationBarsPadding()) }
    }
    }
}

@Composable
private fun UploadFileRow(
    draft: UploadDraft,
    input: String?,
    busy: Boolean,
    showSuggestions: Boolean,
    target: Session?,
    transport: Long,
    library: String,
    directory: String,
    onEdit: () -> Unit,
    onName: (String) -> Unit,
    onRemove: () -> Unit,
) {
    val extension = draft.lockedExtension()
    val stem = input ?: draft.name.removeSuffix(extension)
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        QimengCapsuleTextField(stem, { onEdit(); onName(it) }, enabled = !busy, placeholder = "作品名", singleLine = true,
            modifier = Modifier.weight(1f).onFocusChanged { if (it.isFocused) onEdit() }.testTag("draft-name:${draft.id}"))
        if (extension.isNotEmpty()) {
            Text(extension, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text("锁定", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        TextButton(onClick = onRemove, enabled = !busy) { Text("移除") }
    }
    if (showSuggestions && target != null && draft.bytes > 0) {
        val platformMime = MimeTypeMap.getSingleton().getMimeTypeFromExtension(extension.removePrefix(".").lowercase())
        val kind = if (draft.mimeType.startsWith("video/") || platformMime?.startsWith("video/") == true) "video" else "image"
        FileNameHints(target, transport, library, directory, stem, extension, kind, !busy, onName)
    }
}

@Composable
private fun UploadAssociations(model: LibraryViewModel, target: Session, transport: Long) {
    var expanded by rememberSaveable { mutableStateOf(false) }
    var chooseAuthors by rememberSaveable { mutableStateOf(false) }
    var chooseCollection by rememberSaveable { mutableStateOf(false) }
    var newCollection by rememberSaveable { mutableStateOf(false) }
    val authorIds by model.uploadAuthorIds.collectAsStateWithLifecycle()
    val collectionId by model.uploadCollectionId.collectAsStateWithLifecycle()
    val note by model.uploadNote.collectAsStateWithLifecycle()
    val acquisitionSources by model.uploadSources.collectAsStateWithLifecycle()
    val authorSources by model.uploadAuthorSources.collectAsStateWithLifecycle()
    val libraryId by model.uploadLibrary.collectAsStateWithLifecycle()
    val collections by model.collections.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    QimengSettingsCard {
      Column(Modifier.fillMaxWidth().padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(Modifier.fillMaxWidth().clickable { expanded = !expanded }, verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("作者与来源", style = MaterialTheme.typography.titleSmall)
                Text(if (authorIds.isEmpty()) "可选 · 同一作者的作品可预设，散图可跳过。" else "已选 ${authorIds.size} 位作者",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Text(if (expanded) "收起" else "展开", style = MaterialTheme.typography.labelMedium)
        }
        if (expanded) {
            Text("仅对这次提交的全部待传文件应用所选关联；混合内容可保持未指定。", style = MaterialTheme.typography.bodySmall)
            OutlinedButton(onClick = { chooseAuthors = true }, enabled = !busy) { Text("选择作者") }
            var refreshSources by remember { mutableIntStateOf(0) }
            val options = acquisitionOptions(target, transport, refreshSources)
            if (options.loading && options.data == null) Text("正在读取已有来源…", style = MaterialTheme.typography.bodySmall)
            options.error?.let { ErrorNotice(it) { refreshSources++ } }
            if (authorIds.isNotEmpty()) {
                OutlinedTextField(authorSources, model::setUploadAuthorSources, enabled = !busy,
                    label = { Text("追加到所选作者的来源（每行一条）") }, modifier = Modifier.fillMaxWidth())
                AcquisitionChoiceRow(options.data.orEmpty(), acquisitionTerms(authorSources), !busy, "upload-author-source") {
                    model.setUploadAuthorSources(toggleAcquisition(authorSources, it))
                }
            }
            OutlinedTextField(acquisitionSources, model::setUploadSources, enabled = !busy,
                label = { Text("获取渠道 / 来源（每行一条）") }, modifier = Modifier.fillMaxWidth())
            AcquisitionChoiceRow(options.data.orEmpty(), acquisitionTerms(acquisitionSources), !busy, "upload-asset-source") {
                model.setUploadSources(toggleAcquisition(acquisitionSources, it))
            }
            OutlinedButton(onClick = { model.loadCollections(); chooseCollection = true }, enabled = libraryId.isNotEmpty() && !busy) { Text("组图 / 短剧集合") }
            OutlinedTextField(note, model::setUploadNote, label = { Text("本批备注（原样保存）") }, minLines = 2, modifier = Modifier.fillMaxWidth())
        }
      }
    }
    if (chooseAuthors) {
        var selected by rememberSaveable { mutableStateOf(authorIds) }
        AlertDialog(onDismissRequest = { chooseAuthors = false }, title = { Text("本批关联作者") }, text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("已选 ${selected.size} 位作者", style = MaterialTheme.typography.bodySmall)
                AuthorLookup(target, transport, selected, !busy, "upload-author-search", onPick = { author ->
                    selected = if (author.id in selected) selected - author.id else selected + author.id
                })
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
        if (state.loading && !state.loaded) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
        if (state.loaded && state.items.isEmpty()) item { Text("暂无任务报告") }
        items(state.items, key = { it.id }) { task -> ContentCard {
            Text(task.summary, style = MaterialTheme.typography.titleMedium)
            Text("${stateName(task.state)} · ${task.createdAt}")
            TextButton(onClick = { model.openReport(task.id, task.target) }) { Text("逐项结果") }
        } }
        if (state.cursor != null) item { TextButton(onClick = { model.loadReports(true) }, enabled = !state.loading) { Text("加载更多任务") } }
    }
}

@Composable
fun TaskDetailScreen(model: LibraryViewModel, id: String, target: Session?) {
    val report by model.report.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    var cancelling by rememberSaveable(id) { mutableStateOf(false) }
    LaunchedEffect(id) { if (report?.id != id) model.refreshReport(id) }
    val task = report?.takeIf { it.id == id && sameTarget(it.target, target) }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { TextButton(onClick = { model.refreshReport(id) }) { Text("刷新报告") } }
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
                item.assetId?.let { asset -> TextButton(onClick = { model.openAsset(asset, target = task.target) }) { Text("查看入库结果") } }
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
