package media.qimeng.next.screen

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import media.qimeng.next.LoadState
import media.qimeng.next.model.Author

@Composable
internal fun AuthorSourcesDialog(
    selected: Author,
    state: LoadState<Author>,
    saving: Boolean,
    saveError: String?,
    onRetry: () -> Unit,
    onSave: (Author, List<String>, () -> Unit) -> Unit,
    onClose: () -> Unit,
) {
    val record = state.data?.takeIf { it.id == selected.id }
    var draft by rememberSaveable(selected.id, record?.revision) { mutableStateOf(record?.acquisitionSources.orEmpty().joinToString("\n")) }
    var confirmDiscard by rememberSaveable(selected.id) { mutableStateOf(false) }
    val values = draft.lineSequence().map(String::trim).filter(String::isNotEmpty).distinct().toList()
    val dirty = record != null && values != record.acquisitionSources
    val cancel: () -> Unit = {
        if (!saving) {
            if (dirty) confirmDiscard = true else onClose()
        }
    }
    AlertDialog(
        onDismissRequest = cancel,
        containerColor = MaterialTheme.colorScheme.surface,
        title = { Text("来源 / 出处") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(selected.name, style = MaterialTheme.typography.titleMedium)
                Text("记录获取渠道、平台或个人链接。清空并保存将移除全部来源。", style = MaterialTheme.typography.bodySmall)
                if (state.loading) Text("读取中…", style = MaterialTheme.typography.bodySmall)
                state.error?.let {
                    Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                    TextButton(onClick = onRetry) { Text("重试") }
                }
                saveError?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
                if (record != null) OutlinedTextField(draft, { draft = it }, enabled = !saving,
                    modifier = Modifier.fillMaxWidth().testTag("author-sources-draft"), label = { Text("获取渠道（每行一条）") }, minLines = 3)
            }
        },
        confirmButton = {
            TextButton(onClick = { record?.let { onSave(it, values, onClose) } }, enabled = dirty && !saving && !state.loading) {
                Text(if (saving) "保存中…" else "保存来源")
            }
        },
        dismissButton = { TextButton(onClick = cancel, enabled = !saving) { Text("取消") } },
    )
    if (confirmDiscard) AlertDialog(onDismissRequest = { confirmDiscard = false }, title = { Text("放弃未保存的修改？") },
        confirmButton = { TextButton(onClick = { confirmDiscard = false; onClose() }) { Text("放弃修改") } },
        dismissButton = { TextButton(onClick = { confirmDiscard = false }) { Text("继续编辑") } })
}
