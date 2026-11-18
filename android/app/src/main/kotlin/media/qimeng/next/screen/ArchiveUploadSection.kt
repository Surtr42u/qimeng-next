package media.qimeng.next.screen

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.provider.DocumentsContract
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.ArchiveViewModel
import media.qimeng.next.model.ArchiveOverview
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.QimengPageIntro
import media.qimeng.next.ui.component.QimengTopBar

@Composable
internal fun ArchiveSettingsScreen(back: () -> Unit, model: ArchiveViewModel = viewModel()) {
    val tree by model.tree.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val error by model.error.collectAsStateWithLifecycle()
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        val data = result.data
        if (result.resultCode == Activity.RESULT_OK && data != null) data.data?.let { model.select(it, data.flags) }
    }
    Column(Modifier.fillMaxSize()) {
        QimengTopBar("上传归档文件夹", onBack = back)
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            QimengPageIntro("选择保存手机归档文件的目录。一级子文件夹按名称匹配媒体库。")
            UploadPanel("归档文件夹") {
                Text(tree?.let { DocumentsContract.getTreeDocumentId(Uri.parse(it)).substringAfterLast('/').substringAfterLast(':') } ?: "尚未设置",
                    style = MaterialTheme.typography.bodySmall)
                Button(onClick = { picker.launch(Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or
                    Intent.FLAG_GRANT_WRITE_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION or Intent.FLAG_GRANT_PREFIX_URI_PERMISSION)) }, enabled = !busy) { Text("选择归档文件夹") }
                if (tree != null) TextButton(onClick = { model.select(null) }, enabled = !busy) { Text("清除设置") }
            }
            error?.let { ErrorNotice(it) }
        }
    }
}

@Composable
internal fun ArchiveUploadSection(openSettings: () -> Unit, model: ArchiveViewModel = viewModel()) {
    val tree by model.tree.collectAsStateWithLifecycle()
    val target by model.session.collectAsStateWithLifecycle()
    val overview by model.overview.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val error by model.error.collectAsStateWithLifecycle()
    var confirming by rememberSaveable { mutableStateOf<String?>(null) }
    LaunchedEffect(tree, target) { if (tree != null) model.scan(onlyIfMissing = true) }
    val current = overview?.takeIf { it.target == target && it.tree == tree }
    UploadPanel("归档文件夹", "按库名批量上传已归档的文件。") {
        when {
            tree == null -> {
                Text("还未设置归档目录。设置后，可将按库名归档的文件一键上传到当前服务器。", style = MaterialTheme.typography.bodySmall)
                TextButton(onClick = openSettings) { Text("设置归档文件夹") }
            }
            current?.state in setOf("confirmed", "preparing") -> {
                Text("已入队 ${current?.queuedFiles} / ${current?.files} 个归档文件")
                current?.let { entry -> entry.error?.let { message -> ErrorNotice(message) { model.confirm(entry) } } }
            }
            current?.state == "queued" -> {
                Text("已入队 ${current.files} 个归档文件，传输结果见下方队列。")
                TextButton(onClick = { model.scan() }, enabled = !busy) { Text("重新扫描") }
            }
            else -> {
                if (busy) Text("正在扫描归档文件夹…", style = MaterialTheme.typography.bodySmall)
                if (current != null) Text("可上传 ${current.files} 个文件，未匹配 ${current.excludedFolders} 个文件夹。")
                Button(onClick = { confirming = current?.id }, enabled = !busy && current != null && current.state == "preview" && current.files > 0) { Text("一键上传") }
                TextButton(onClick = { model.scan() }, enabled = !busy) { Text("重新扫描") }
            }
        }
        error?.let { ErrorNotice(it) }
    }
    if (current != null && confirming == current.id) ArchiveConfirm(current, { confirming = null }) { model.confirm(current); confirming = null }
}

@Composable
private fun ArchiveConfirm(plan: ArchiveOverview, dismiss: () -> Unit, submit: () -> Unit) {
    AlertDialog(onDismissRequest = dismiss, title = { Text("确认一键上传") }, text = { Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("将 ${plan.files} 个归档文件上传到「${plan.target.label}」，库内子目录保持原层级。")
        if (plan.excludedFolders > 0) Text("未匹配文件夹（不上传）：${plan.examples.joinToString("、")}（共 ${plan.excludedFolders} 个）")
        Text("重新扫描后若清单有变化，再次确认会把清单中仍在原位的文件一起上传。", style = MaterialTheme.typography.bodySmall)
        Text("来源回收需先核验本批的独立备份和原始文件。", style = MaterialTheme.typography.bodySmall)
    } }, confirmButton = { TextButton(onClick = submit) { Text("确认上传") } }, dismissButton = { TextButton(onClick = dismiss) { Text("取消") } })
}
