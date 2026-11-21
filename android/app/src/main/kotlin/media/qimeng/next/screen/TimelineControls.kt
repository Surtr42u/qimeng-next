package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.media3.exoplayer.ExoPlayer
import media.qimeng.next.ManagementViewModel
import media.qimeng.next.model.Asset
import media.qimeng.next.model.TimelineMark
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice

@Composable
internal fun TimelineControls(asset: Asset, player: ExoPlayer, model: ManagementViewModel = viewModel()) {
    val marks by model.marks.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val error by model.error.collectAsStateWithLifecycle()
    var expanded by rememberSaveable(asset.id) { mutableStateOf(false) }
    var edit by remember { mutableStateOf<TimelineMark?>(null) }
    var editor by rememberSaveable(asset.id) { mutableStateOf(false) }
    var startPosition by rememberSaveable(asset.id) { mutableLongStateOf(0) }
    var deleting by remember { mutableStateOf<TimelineMark?>(null) }
    LaunchedEffect(asset.id, expanded) { if (expanded) model.loadMarks(asset) }
    ContentCard(Modifier.padding(horizontal = 16.dp)) {
        Row {
            TextButton(onClick = { expanded = !expanded }) { Text(if (expanded) "收起时间轴标记" else "时间轴标记") }
            TextButton(onClick = { edit = null; startPosition = player.currentPosition; editor = true }, enabled = !busy) { Text("标记当前位置") }
        }
        if (expanded) {
            marks.error?.let { ErrorNotice(it) { model.loadMarks(asset) } }
            error?.let { ErrorNotice(it) }
            if (marks.loading && !marks.loaded) LinearProgressIndicator(Modifier.fillMaxWidth())
            marks.items.forEach { mark ->
                Text("${mark.positionMs / 1000} 秒 · ${mark.label}")
                if (mark.contentVersion != asset.version) Text("属于旧内容版本 ${mark.contentVersion}，位置不映射到当前视频", style = MaterialTheme.typography.bodySmall)
                Row {
                    TextButton(onClick = { player.seekTo(mark.positionMs) }, enabled = mark.contentVersion == asset.version) { Text("跳转") }
                    TextButton(onClick = { edit = mark; startPosition = mark.positionMs; editor = true }, enabled = !busy) { Text("编辑") }
                    TextButton(onClick = { deleting = mark }, enabled = !busy) { Text("删除") }
                }
            }
            if (marks.cursor != null) TextButton(onClick = { model.loadMarks(asset, true) }, enabled = !marks.loading) { Text("更多标记") }
            if (marks.loaded && marks.items.isEmpty()) Text("还没有时间轴标记")
        }
    }
    if (editor) {
        var position by rememberSaveable { mutableStateOf((startPosition / 1000.0).toString()) }
        var label by rememberSaveable { mutableStateOf(edit?.label.orEmpty()) }
        // 默认色随当前主题主色（本项目为中性灰），不再写入 M3 基线紫；服务器仍按 #RRGGBB 保存。
        val defaultColor = markColorHex(MaterialTheme.colorScheme.primary)
        var color by rememberSaveable { mutableStateOf(edit?.color ?: defaultColor) }
        val seconds = position.toDoubleOrNull()?.takeIf { it.isFinite() && it >= 0 && it <= Long.MAX_VALUE / 1000.0 }
        AlertDialog(onDismissRequest = { if (!busy) editor = false }, title = { Text("时间轴标记") }, text = {
            Column(Modifier.heightIn(max = 400.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedTextField(label, { label = it }, label = { Text("名称") })
                OutlinedTextField(position, { position = it }, label = { Text("位置（秒）") }, isError = seconds == null)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    listOf("#6750A4" to "紫", "#4285F4" to "蓝", "#E8B24E" to "黄", "#52A87C" to "绿").forEach { (value, title) ->
                        FilterChip(color == value, { color = value }, label = { Text(title) })
                    }
                }
                error?.let { ErrorNotice(it) }
            }
        }, confirmButton = { TextButton(onClick = { model.saveMark(asset, edit, (seconds!! * 1000).toLong(), label.trim(), color) { editor = false; expanded = true } },
            enabled = !busy && seconds != null && label.isNotBlank()) { Text("保存") } }, dismissButton = { TextButton(onClick = { editor = false }, enabled = !busy) { Text("取消") } })
    }
    deleting?.let { mark -> AlertDialog(onDismissRequest = { deleting = null }, title = { Text("删除时间轴标记？") }, text = { Text(mark.label) },
        confirmButton = { TextButton(onClick = { model.deleteMark(asset, mark); deleting = null }, enabled = !busy) { Text("删除") } },
        dismissButton = { TextButton(onClick = { deleting = null }) { Text("取消") } }) }
}

/** 服务器按 #RRGGBB 存标记颜色；用 Locale.ROOT 避免地区化数字影响十六进制输出。 */
internal fun markColorHex(color: Color): String = String.format(java.util.Locale.ROOT, "#%06X", color.toArgb() and 0xFFFFFF)
