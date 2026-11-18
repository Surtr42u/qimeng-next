package media.qimeng.next.screen

import android.content.ClipData
import android.content.ClipboardManager
import android.widget.Toast
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.DeviceDeliveryViewModel
import media.qimeng.next.ui.component.QimengCapsuleTextField
import media.qimeng.next.ui.icon.CopyIcon

@Composable
internal fun DeviceTargetValue(label: String, value: String) {
    val context = LocalContext.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.weight(1f)) { SelectionContainer { Text("$label $value", style = MaterialTheme.typography.bodySmall) } }
        IconButton(onClick = {
            context.getSystemService(ClipboardManager::class.java)?.let { clipboard ->
                clipboard.setPrimaryClip(ClipData.newPlainText(label, value))
                Toast.makeText(context, "已复制$label", Toast.LENGTH_SHORT).show()
            }
        }) { Icon(CopyIcon, "复制$label") }
    }
}

@Composable
internal fun DeviceTargetVideoPicker(instanceId: String, model: DeviceDeliveryViewModel, busy: Boolean) {
    var open by rememberSaveable(instanceId) { mutableStateOf(false) }
    var query by rememberSaveable(instanceId) { mutableStateOf("") }
    val list by model.videos.collectAsStateWithLifecycle()
    TextButton(onClick = { open = !open; if (open) model.searchVideos(query) }, enabled = !busy) {
        Text(if (open) "收起目标视频" else "选择本机目标视频")
    }
    if (!open) return
    QimengCapsuleTextField(query, { query = it }, Modifier.fillMaxWidth().testTag("device-target-video-search"),
        placeholder = "按名称搜索本机视频", singleLine = true, enabled = !busy)
    TextButton(onClick = { model.searchVideos(query) }, enabled = !busy) { Text("搜索视频") }
    list?.let { result ->
        result.selected?.let { video ->
            Text("${result.libraryName} · ${video.relativePath}", style = MaterialTheme.typography.titleSmall)
            DeviceTargetValue("目标视频 ID", video.id)
            DeviceTargetValue("目标内容版本", video.version.toString())
        }
        if (result.page.items.isEmpty()) Text("没有符合条件的本机视频", style = MaterialTheme.typography.bodySmall)
        result.page.items.forEach { video ->
            TextButton(onClick = { model.chooseVideo(video) }, enabled = !busy) {
                Column { Text(video.name); Text("${video.relativePath} · 影库 ${video.libraryId.take(8)}", style = MaterialTheme.typography.bodySmall) }
            }
        }
        if (result.page.nextCursor != null && result.query == query.trim()) {
            TextButton(onClick = { model.searchVideos(query, next = true) }, enabled = !busy) { Text("下一页视频") }
        }
    }
}
