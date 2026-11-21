package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice

@Composable
internal fun AuthorTasksScreen(model: LibraryViewModel) {
    val state by model.authorTasks.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    var revoke by rememberSaveable { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) { if (!state.loaded) model.loadAuthorTasks() }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { TextButton(onClick = { model.loadAuthorTasks() }) { Text("刷新作者任务") } }
        if (state.loading && !state.loaded) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
        state.error?.let { item { ErrorNotice(it) { model.loadAuthorTasks() } } }
        if (state.loaded && state.items.isEmpty()) item { Text("暂无作者资料任务") }
        items(state.items, key = { it.id }) { task -> ContentCard {
            Text("作者资料 · ${authorTaskStateName(task.state)}", style = MaterialTheme.typography.titleMedium)
            Text("可修改：${task.allowedFields.joinToString("、") { authorFieldName(it) }.ifEmpty { "无直接写入字段" }}")
            if (task.candidateBlocks.isNotEmpty()) Text("可提交候选：${task.candidateBlocks.joinToString("、") { authorFieldName(it) }}")
            Text("已使用 ${task.usedUpdates} / ${task.maxUpdates} 次更新 · ${if (task.allowClear) "允许清空已授权字段" else "不允许清空字段"}")
            Text("创建：${task.createdAt}\n到期：${task.expiresAt}", style = MaterialTheme.typography.bodySmall)
            Row {
                TextButton(onClick = { model.openAuthor(task.authorId) }) { Text("查看作者档案") }
                if (task.state == "authorized") TextButton(onClick = { revoke = task.id }, enabled = !busy) { Text("撤销授权") }
            }
        } }
        if (state.cursor != null) item { TextButton(onClick = { model.loadAuthorTasks(true) }, enabled = !state.loading) { Text("加载更多作者任务") } }
    }
    revoke?.let { id -> AlertDialog(onDismissRequest = { revoke = null }, title = { Text("撤销此作者资料任务？") },
        text = { Text("执行器将不能继续使用本任务授权，已保存的资料保持。") },
        confirmButton = { TextButton(onClick = { model.cancelAuthorTask(id); revoke = null }, enabled = !busy) { Text("撤销授权") } },
        dismissButton = { TextButton(onClick = { revoke = null }) { Text("取消") } }) }
}

private fun authorTaskStateName(value: String) = mapOf("authorized" to "授权有效", "completed" to "已完成", "cancelled" to "已撤销", "expired" to "已过期")[value] ?: "状态待核对"
private fun authorFieldName(value: String) = mapOf("name" to "名称", "biography" to "简介", "homepage" to "主页", "aliases" to "别名",
    "x" to "X 资料", "basic" to "基本资料", "accounts" to "其他账号")[value] ?: value
