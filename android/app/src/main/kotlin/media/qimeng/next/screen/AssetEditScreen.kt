package media.qimeng.next.screen

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.AssetEditorState
import media.qimeng.next.AssetEditorTarget
import media.qimeng.next.AssetEditorViewModel
import media.qimeng.next.R
import media.qimeng.next.model.Author
import media.qimeng.next.model.acquisitionTerms
import media.qimeng.next.model.validFileStem
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.QimengCapsuleTextField
import media.qimeng.next.ui.component.QimengSegPill
import media.qimeng.next.ui.component.QimengSettingsCard
import media.qimeng.next.ui.component.QimengTopBar

@Composable
internal fun AssetEditScreen(
    scope: AssetEditorTarget,
    onBack: () -> Unit,
    onMore: () -> Unit,
    model: AssetEditorViewModel = viewModel(),
) {
    val current by model.state.collectAsStateWithLifecycle()
    val state = current.takeIf { it.target == scope }
    val asset = state?.asset
    val enabled = state != null && !state.loading && !state.saving
    var requestedReload by rememberSaveable(scope.id, scope.session.instanceId) { mutableStateOf(false) }
    var requestedBack by rememberSaveable(scope.id, scope.session.instanceId) { mutableStateOf(false) }
    var sourceRequest by remember(scope) { mutableIntStateOf(0) }
    val sources = if (asset != null) acquisitionOptions(scope.session, scope.transport, sourceRequest) else null
    val options = sources?.data.orEmpty()
    val leave = { model.close(); onBack() }
    val requestBack: () -> Unit = {
        if (state?.saving != true) {
            if (state?.dirty == true) requestedBack = true else leave()
        }
    }
    LaunchedEffect(scope) { model.open(scope) }
    LaunchedEffect(state?.saved) { if (state?.saved == true) leave() }
    BackHandler(onBack = requestBack)

    Column(Modifier.fillMaxSize()) {
        QimengTopBar(stringResource(R.string.asset_edit_title), onBack = requestBack)
        LazyColumn(Modifier.fillMaxSize().imePadding().testTag("asset-editor-list"),
            verticalArrangement = Arrangement.spacedBy(12.dp),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp)) {
            item {
                if (state?.error != null) ErrorNotice(state.error)
                if (state?.savedSteps?.isNotEmpty() == true && !state.saved) {
                    Text("已保存：${state.savedSteps.joinToString("、")}", style = MaterialTheme.typography.bodySmall)
                }
                if (asset == null && state?.error != null) TextButton(onClick = model::reload, enabled = enabled) { Text("重试读取") }
                if (state?.conflict == true) TextButton(onClick = {
                    if (state.dirty) requestedReload = true else model.reload()
                }, enabled = enabled) { Text("重新读取服务端版本") }
                sources?.error?.let { ErrorNotice(it) { sourceRequest++ } }
                if (state == null || state.loading) LinearProgressIndicator(Modifier.fillMaxWidth())
            }
            if (state != null && asset != null) {
                item { AssetFileNameEditor(scope, state, enabled, model::setStem) }
                item {
                    AssetEditCard {
                        Text("媒体获取渠道", style = MaterialTheme.typography.titleMedium)
                        SourceTextEditor(state.sourceText, "asset-editor-sources", enabled, model::setAssetSources)
                        AcquisitionChoiceRow(options, acquisitionTerms(state.sourceText), enabled, "asset-editor-source-choice") {
                            model.setAssetSources(toggleAcquisition(state.sourceText, it))
                        }
                    }
                }
                item {
                    Text("已关联作者（${state.authorIds.size}）", style = MaterialTheme.typography.titleMedium)
                    if (state.authorIds.isEmpty()) Text(stringResource(R.string.asset_edit_no_authors), style = MaterialTheme.typography.bodySmall)
                }
                items(state.authorIds, key = { it }) { id ->
                    val author = state.authors[id]
                    if (author == null) {
                        Text("新关联作者已保存", style = MaterialTheme.typography.bodySmall)
                    } else {
                        AssetAuthorEditor(author, state.authorSourceText[id], options, enabled,
                            { model.setAuthorSources(id, it) }, { model.removeAuthor(id) })
                    }
                }
                item { AssetAuthorPicker(scope, state, model, enabled) }
                item {
                    Button(onClick = model::save, enabled = state.canSave, modifier = Modifier.fillMaxWidth()) {
                        Text(stringResource(if (state.saving) R.string.asset_edit_saving else R.string.asset_edit_save))
                    }
                    TextButton(onClick = { model.close(); onMore() }, enabled = enabled && !state.dirty) { Text("更多媒体管理") }
                    Spacer(Modifier.navigationBarsPadding())
                }
            }
        }
    }
    if (requestedReload || requestedBack) AlertDialog(
        onDismissRequest = { requestedReload = false; requestedBack = false },
        title = { Text(stringResource(R.string.asset_edit_discard_title)) },
        text = {
            Text(if (requestedReload) "读取成功后将采用服务端版本，并放弃本次未保存的输入。"
                else stringResource(R.string.asset_edit_discard_desc))
        },
        confirmButton = { TextButton(onClick = {
            if (requestedReload) model.reload() else leave()
            requestedReload = false
            requestedBack = false
        }) { Text(if (requestedReload) "放弃并读取" else stringResource(R.string.asset_edit_discard_confirm)) } },
        dismissButton = { TextButton(onClick = { requestedReload = false; requestedBack = false }) { Text(stringResource(R.string.asset_edit_discard_cancel)) } },
    )
}

@Composable
private fun AssetFileNameEditor(scope: AssetEditorTarget, state: AssetEditorState, enabled: Boolean, change: (String) -> Unit) {
    val asset = requireNotNull(state.asset)
    var suggesting by rememberSaveable(asset.id, scope.session.instanceId) { mutableStateOf(false) }
    AssetEditCard {
        Text("文件名", style = MaterialTheme.typography.titleMedium)
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            QimengCapsuleTextField(state.stem, { suggesting = true; change(it) }, enabled = enabled,
                placeholder = "输入文件名", singleLine = true,
                modifier = Modifier.weight(1f).onFocusChanged { if (it.isFocused) suggesting = true }.testTag("asset-editor-name"))
            if (state.extension.isNotEmpty()) {
                Text(state.extension, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text("锁定", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        if (!validFileStem(state.stem, state.extension)) Text("请输入有效的文件名。", color = MaterialTheme.colorScheme.error,
            style = MaterialTheme.typography.bodySmall)
        if (suggesting) FileNameHints(scope.session, scope.transport, asset.libraryId, asset.relativePath.substringBeforeLast('/', ""),
            state.stem, state.extension, asset.mediaType, enabled, change)
    }
}

@Composable
private fun AssetAuthorEditor(author: Author, draft: String?, options: List<String>, enabled: Boolean, change: (String) -> Unit, remove: () -> Unit) {
    val source = draft ?: author.acquisitionSources.joinToString("\n")
    AssetEditCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(author.name, Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
            if (author.kind == "cos") Text("COS 目录作者", style = MaterialTheme.typography.labelSmall)
            else TextButton(onClick = remove, enabled = enabled) { Text(stringResource(R.string.asset_edit_remove_author)) }
        }
        SourceTextEditor(source, "asset-editor-author-source:${author.id}", enabled, change)
        AcquisitionChoiceRow(options, acquisitionTerms(source), enabled, "asset-editor-author-choice:${author.id}") {
            change(toggleAcquisition(source, it))
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun AssetAuthorPicker(scope: AssetEditorTarget, state: AssetEditorState, model: AssetEditorViewModel, enabled: Boolean) {
    AssetEditCard {
        Text(stringResource(R.string.asset_edit_add_author_section), style = MaterialTheme.typography.titleMedium)
        AuthorLookup(scope.session, scope.transport, state.authorIds, enabled, "asset-editor-author-search", model::pickAuthor, model::addName)
        if (state.authorIds.size + state.newNames.size >= 20) Text("每项最多关联 20 位作者。", style = MaterialTheme.typography.bodySmall)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            state.newNames.forEach { name -> QimengSegPill("新建：$name ✕", true, { model.removeName(name) }, enabled = enabled) }
        }
    }
}

@Composable
private fun AssetEditCard(content: @Composable ColumnScope.() -> Unit) {
    QimengSettingsCard {
        Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp), content = content)
    }
}

@Composable
private fun SourceTextEditor(value: String, tag: String, enabled: Boolean, change: (String) -> Unit) {
    OutlinedTextField(value, change, enabled = enabled, modifier = Modifier.fillMaxWidth().testTag(tag),
        label = { Text("获取渠道 / 来源（每行一条）") }, minLines = 2)
}
