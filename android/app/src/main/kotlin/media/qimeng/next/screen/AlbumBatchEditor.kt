package media.qimeng.next.screen

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SheetValue
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.AlbumBatchChange
import media.qimeng.next.AlbumBatchViewModel
import media.qimeng.next.AlbumSelectionState
import media.qimeng.next.model.Asset
import media.qimeng.next.model.Author
import media.qimeng.next.model.Session
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.QimengChipRow
import media.qimeng.next.ui.component.QimengPill

@Composable
internal fun AlbumSelectionBar(model: AlbumBatchViewModel, state: AlbumSelectionState, loaded: List<Asset>) {
    val allLoaded = loaded.isNotEmpty() && loaded.all { it.id in state.selected }
    Surface(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
        shape = RoundedCornerShape(16.dp),
        color = MaterialTheme.colorScheme.surfaceContainerHigh,
        shadowElevation = 2.dp,
    ) {
        Row(Modifier.padding(horizontal = 12.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("已选 ${state.selected.size} 项", Modifier.weight(1f),
                style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            TextButton(onClick = { if (allLoaded) model.clear() else model.selectLoaded(loaded) }, enabled = loaded.isNotEmpty()) {
                Text(if (allLoaded) "清空" else "全选")
            }
            TextButton(onClick = model::exit) { Text("退出") }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun AlbumBatchSheet(model: AlbumBatchViewModel, state: AlbumSelectionState, onUpdated: (Session) -> Unit) {
    val running by rememberUpdatedState(state.progress != null)
    val confirm = remember { { value: SheetValue -> value != SheetValue.Hidden || !running } }
    val sheet = rememberModalBottomSheetState(skipPartiallyExpanded = true, confirmValueChange = confirm)
    ModalBottomSheet(onDismissRequest = model::closeSheet, sheetState = sheet) {
        Column(Modifier.fillMaxWidth().imePadding().padding(start = 16.dp, end = 16.dp, bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("批量设置作者与来源", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
            if (state.progress == null && state.report.isEmpty()) Column(
                Modifier.fillMaxWidth().verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) { AlbumBatchDraft(model, state.selected.size, onUpdated) }
            else AlbumBatchReport(model, state, onUpdated)
        }
    }
}

@Composable
private fun AlbumBatchDraft(model: AlbumBatchViewModel, count: Int, onUpdated: (Session) -> Unit) {
    var editingSources by rememberSaveable { mutableStateOf(false) }
    var query by rememberSaveable { mutableStateOf("") }
    var author by rememberSaveable { mutableStateOf<Author?>(null) }
    var sources by rememberSaveable { mutableStateOf("") }
    val choices by model.authors.collectAsStateWithLifecycle()
    val focus = LocalFocusManager.current
    LaunchedEffect(editingSources, query, author) {
        if (!editingSources && author == null) model.findAuthors(query)
    }
    QimengChipRow(
        listOf(QimengPill("设置作者", !editingSources), QimengPill("获取来源", editingSources)),
        onPillClick = { editingSources = it == 1 },
    )
    if (editingSources) {
        Text("向每项现有的获取来源追加以下条目，已有来源会保留。", style = MaterialTheme.typography.bodySmall)
        OutlinedTextField(sources, { sources = it }, Modifier.fillMaxWidth().testTag("batch-acquisition-input"),
            label = { Text("获取来源（每行一条）") }, minLines = 2, maxLines = 4)
    } else {
        Text("替换每项的常规作者。COS 目录作者保留。", style = MaterialTheme.typography.bodySmall)
        val selected = author
        if (selected != null) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(selected.name, Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
                TextButton(onClick = { author = null }) { Text("重选") }
            }
        } else {
            OutlinedTextField(query, { query = it }, Modifier.fillMaxWidth().testTag("batch-author-input"),
                label = { Text("搜索已有常规作者") }, singleLine = true)
            if (choices.loading) LinearProgressIndicator(Modifier.fillMaxWidth())
            choices.error?.let { ErrorNotice(it) { model.findAuthors(query) } }
            if (choices.data?.isEmpty() == true) Text("没有匹配的常规作者", style = MaterialTheme.typography.bodySmall)
            LazyColumn(Modifier.fillMaxWidth().heightIn(max = 136.dp)) {
                items(choices.data.orEmpty(), key = Author::id) { option ->
                    Text(option.name, Modifier.fillMaxWidth().clickable {
                        focus.clearFocus()
                        author = option
                    }.padding(horizontal = 8.dp, vertical = 12.dp))
                }
            }
        }
    }
    val channels = sources.lineSequence().map(String::trim).filter(String::isNotEmpty).distinct().toList()
    val operation = if (editingSources) channels.takeIf { it.isNotEmpty() }?.let { AlbumBatchChange.AppendAcquisition(it) }
        else author?.let { AlbumBatchChange.ReplaceAuthor(it) }
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
        TextButton(onClick = model::closeSheet) { Text("取消") }
        Button(enabled = operation != null, onClick = {
            focus.clearFocus()
            operation?.let { model.submit(it, onUpdated) }
        }) { Text(if (editingSources) "追加到 $count 项" else "替换 $count 项作者") }
    }
}

@Composable
private fun AlbumBatchReport(model: AlbumBatchViewModel, state: AlbumSelectionState, onUpdated: (Session) -> Unit) {
    val progress = state.progress
    if (progress != null) {
        val action = if (progress.reviewing) "核对" else "处理"
        Text("正在$action ${progress.completed} / ${progress.total} 项")
        LinearProgressIndicator(progress = { progress.completed.toFloat() / progress.total }, modifier = Modifier.fillMaxWidth())
        return
    }
    val failed = state.report.count { it.error != null }
    val retry = state.report.count { it.error != null && !it.conflict }
    Text("成功 ${state.report.size - failed} 项 · 失败 $failed 项", style = MaterialTheme.typography.titleMedium)
    LazyColumn(Modifier.fillMaxWidth().heightIn(max = 300.dp).testTag("album-batch-report")) {
        items(state.report, key = { it.asset.id }) { result ->
            Column(Modifier.fillMaxWidth().padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(result.asset.relativePath, style = MaterialTheme.typography.bodyMedium)
                Text(result.error ?: "已完成", style = MaterialTheme.typography.bodySmall,
                    color = if (result.error != null && !result.reviewed) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant)
                if (result.reviewed) Text(model.reviewedDetails(result.asset), style = MaterialTheme.typography.bodySmall)
            }
            HorizontalDivider()
        }
    }
    if (state.report.any { it.conflict }) TextButton(onClick = model::reviewConflicts) { Text("核对冲突项") }
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
        TextButton(onClick = model::closeSheet) { Text(if (failed == 0) "完成" else "关闭") }
        if (retry > 0) Button(onClick = { model.retry(onUpdated) }) {
            Text(if (state.report.any { it.reviewed }) "确认重试 $retry 项" else "重试 $retry 项")
        }
    }
}
