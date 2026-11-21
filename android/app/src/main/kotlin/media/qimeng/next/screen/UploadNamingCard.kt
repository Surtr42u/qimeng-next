package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import media.qimeng.next.NamingState
import media.qimeng.next.model.NamingRequest
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice

@Composable
internal fun UploadNamingCard(enabled: Boolean, family: String, collectionId: String?, numberClass: String,
    request: NamingRequest?, state: NamingState, busy: Boolean, onEnabled: (Boolean) -> Unit,
    onFamily: (String) -> Unit, onClass: (String) -> Unit, onRetry: () -> Unit) {
    val active = enabled || collectionId != null
    // 预览按内容而非请求身份保留：新输入在防抖/请求期间继续显示上一份，只有状态里没有预览时才让位给进度条。
    val inspection = state.inspection
    val stale = inspection != null && state.inspectionRequest != request
    ContentCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(active, onEnabled, enabled = !busy && collectionId == null)
            Text(if (collectionId == null) "新作品 · 自动续号" else "所选集合 · 按现有序列追加", style = MaterialTheme.typography.titleMedium)
        }
        if (active) {
            OutlinedTextField(family, onFamily, enabled = !busy, label = { Text(if (collectionId == null) "作品基名" else "首个作品基名（已有集合以服务器为准）") },
                supportingText = { Text(when {
                    // 说明写在既有支撑文案里，不新增一行，避免预览区在每次输入时上下位移。
                    state.loading && stale -> "预览对应上一次输入，正在更新…"
                    stale && state.error != null -> "预览对应上一次输入。"
                    else -> "输入名称后自动补全；不需要填写作品编号。"
                }) }, modifier = Modifier.fillMaxWidth())
            if (collectionId == null) Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(numberClass == "image", { onClass("image") }, enabled = !busy, label = { Text("图片建议") })
                FilterChip(numberClass == "video", { onClass("video") }, enabled = !busy, label = { Text("视频建议") })
            }
            inspection?.suggestions?.forEach { suggestion ->
                TextButton(onClick = { onFamily(suggestion.family) }, enabled = !busy) { Text(suggestion.example) }
            }
            if (inspection?.truncated == true) Text("还有其他匹配，请继续输入缩小范围。", style = MaterialTheme.typography.bodySmall)
            // 只有屏幕上还没有任何预览时才显示进度条；已有预览时由输入框支撑文案说明正在更新。
            if (state.loading && inspection == null) LinearProgressIndicator(Modifier.fillMaxWidth())
            state.error?.let { ErrorNotice(it, onRetry) }
            inspection?.position?.let { position ->
                Text(when {
                    position.nextChapter != null -> "短剧追加 · 从第 ${position.nextChapter} 章开始"
                    position.nextPage != null -> "组图补页 · 作品 ${position.currentWork}，从第 ${position.nextPage} 页开始"
                    else -> "新作品 · 下一${if (position.numberClass == "video") "视频" else "图片"}作品号 ${position.nextWork}"
                }, style = MaterialTheme.typography.titleSmall)
                Text("${position.family} · ${position.directory.ifEmpty { "影库根目录" }}", style = MaterialTheme.typography.bodySmall)
            }
            if (inspection?.unsupportedInputs == true) Text("本批含无法预览名称的文件。有效文件仍按所选规则命名，结果见本批报告。", style = MaterialTheme.typography.bodySmall)
            inspection?.preview?.let { preview ->
                Text("本批名称预览", style = MaterialTheme.typography.titleSmall)
                preview.names.forEach { Text(it, style = MaterialTheme.typography.bodySmall) }
                Text(if (preview.reserved) "服务器已保留此预览。" else "预览不占号。最终入库会再次检查，以任务报告中的实际名称为准。", style = MaterialTheme.typography.bodySmall)
            }
            if (collectionId != null) Text("依选择顺序追加。前一项失败时先处理该项，后续文件不会跳过它分配页码或章节。", style = MaterialTheme.typography.bodySmall)
        }
    }
}
