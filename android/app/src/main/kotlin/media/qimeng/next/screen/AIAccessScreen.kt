package media.qimeng.next.screen

import android.app.Activity
import android.content.ClipData
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.os.Build
import android.os.PersistableBundle
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.DialogProperties
import androidx.compose.ui.window.SecureFlagPolicy
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.AIAccessViewModel
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.AIAccessPolicy
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice

@Composable
fun AIAccessScreen(library: LibraryViewModel, model: AIAccessViewModel = viewModel()) {
    val state by model.state.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val secret by model.secret.collectAsStateWithLifecycle()
    val reconcile by model.reconcileRequired.collectAsStateWithLifecycle()
    val activity = LocalContext.current as? Activity
    var creating by rememberSaveable { mutableStateOf(false) }
    var revoking by rememberSaveable { mutableStateOf<String?>(null) }
    BackHandler(busy) { }
    LaunchedEffect(Unit) { if (state.data == null) model.refresh() }
    LaunchedEffect(secret) { if (secret != null) creating = false }
    DisposableEffect(model) { onDispose { if (activity?.isChangingConfigurations != true) model.dismissSecret() } }
    Column(Modifier.fillMaxSize()) {
        PageHeader("AI 接入", library::back, backEnabled = !busy)
        LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            item { ContentCard {
                Text("连接你使用的 AI 工具", style = MaterialTheme.typography.titleMedium)
                Text("默认只读可查询全库。影库与作者选择限制写入范围，不限制读取。正常授权任务由工具自动接续，无需逐批复制任务凭据。")
                Row {
                    Button(onClick = { model.clearCreationError(); creating = true }, enabled = !busy && !reconcile) { Text("新增 AI 接入") }
                    TextButton(onClick = model::refresh, enabled = !busy) { Text("刷新") }
                }
            } }
            if (state.loading || busy) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
            if (reconcile) item { ErrorNotice("上次创建结果未确认，请刷新核对连接后再继续。", model::refresh) }
            state.error?.let { item { ErrorNotice(it, model::refresh) } }
            if (state.data?.isEmpty() == true) item { Text("尚未配置 AI 接入") }
            items(state.data.orEmpty(), key = { it.id }) { grant -> ContentCard {
                Text(grant.label, style = MaterialTheme.typography.titleMedium)
                Text("${mapOf("active" to "有效", "revoked" to "已撤销", "expired" to "已过期")[grant.state] ?: "状态待核对"} · 到期 ${grant.expiresAt}")
                Text("全库只读 · ${if (grant.canIngest) "可新增媒体" else "不可新增媒体"} · ${if (grant.canResearch) "可研究" else "不可研究"} · ${if (grant.canOrganize) "可整理" else "不可整理"}")
                Text("写入范围：${grant.libraryIds.size} 个影库，${if (grant.allAuthors) "全部可维护作者" else "${grant.authorIds.size} 位作者"}")
                if (grant.allowedFields.isNotEmpty()) Text("作者字段：${grant.allowedFields.joinToString("、") { fieldName(it) }}")
                Text("每日 ${grant.maxTasksPerDay} 个任务；每任务最多 ${grant.maxUpdates} 次资料更新、${grant.maxResearchCalls} 次研究", style = MaterialTheme.typography.bodySmall)
                if (grant.state == "active") TextButton(onClick = { revoking = grant.id }, enabled = !busy) { Text("撤销接入") }
            } }
        }
    }
    if (creating) AIAccessForm(library, model) { creating = false }
    revoking?.let { id -> AlertDialog(onDismissRequest = { revoking = null }, title = { Text("撤销此 AI 接入？") },
        text = { Text("该连接及其任务凭据将失效，已经保存的媒体和资料保留。") },
        confirmButton = { TextButton(onClick = { model.revoke(id); revoking = null }, enabled = !busy) { Text("撤销接入") } },
        dismissButton = { TextButton(onClick = { revoking = null }) { Text("取消") } }) }
    secret?.let { token -> OneTimeCredential(token, model::dismissSecret) }
}

@Composable
private fun AIAccessForm(library: LibraryViewModel, model: AIAccessViewModel, close: () -> Unit) {
    val libraries by library.libraries.collectAsStateWithLifecycle()
    val authors by library.authors.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val error by model.creationError.collectAsStateWithLifecycle()
    var label by rememberSaveable { mutableStateOf("我的 AI 助手") }
    var libraryIds by rememberSaveable { mutableStateOf(emptyList<String>()) }
    var authorIds by rememberSaveable { mutableStateOf(emptyList<String>()) }
    var allAuthors by rememberSaveable { mutableStateOf(false) }
    var fields by rememberSaveable { mutableStateOf(emptyList<String>()) }
    var blocks by rememberSaveable { mutableStateOf(emptyList<String>()) }
    var canIngest by rememberSaveable { mutableStateOf(false) }
    var canResearch by rememberSaveable { mutableStateOf(false) }
    var canOrganize by rememberSaveable { mutableStateOf(false) }
    var allowClear by rememberSaveable { mutableStateOf(false) }
    var days by rememberSaveable { mutableStateOf("30") }
    var tasks by rememberSaveable { mutableStateOf("20") }
    var updates by rememberSaveable { mutableStateOf("1") }
    var research by rememberSaveable { mutableStateOf("8") }
    var calls by rememberSaveable { mutableStateOf("100") }
    var minutes by rememberSaveable { mutableStateOf("60") }
    LaunchedEffect(Unit) { library.refreshConnection(); library.loadAuthors() }
    val validDays = days.toIntOrNull()?.takeIf { it in 1..365 }
    val dailyTasks = tasks.toIntOrNull()?.takeIf { it in 1..200 }
    val maxUpdates = updates.toIntOrNull()?.takeIf { it in 1..10 }
    val maxResearch = research.toIntOrNull()?.takeIf { it in 0..100 }
    val maxCalls = calls.toIntOrNull()?.takeIf { it in 10..10000 }
    val duration = minutes.toIntOrNull()?.takeIf { it in 1..1440 }
    LaunchedEffect(label, libraryIds, authorIds, allAuthors, fields, blocks, canIngest, canResearch, canOrganize, allowClear, days, tasks, updates, research, calls, minutes) {
        model.clearCreationError()
    }
    val writesNeedLibrary = canIngest || canResearch || canOrganize
    val writesNeedAuthor = fields.isNotEmpty() || blocks.isNotEmpty()
    val valid = label.isNotBlank() && label.length <= 80 && libraryIds.size <= 100 && authorIds.size <= 100 && validDays != null && dailyTasks != null && maxUpdates != null && maxResearch != null && maxCalls != null && duration != null &&
        (!writesNeedLibrary || libraryIds.isNotEmpty()) && (!writesNeedAuthor || allAuthors || authorIds.isNotEmpty())
    AlertDialog(onDismissRequest = { if (!busy) close() }, title = { Text("新增 AI 接入") }, text = {
        Column(Modifier.heightIn(max = 520.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            OutlinedTextField(label, { label = it }, label = { Text("连接名称") }, enabled = !busy)
            Text("只读查询全库始终开放；下面仅增加你选择的写入权限。", style = MaterialTheme.typography.bodySmall)
            PermissionRow("新增媒体", canIngest, !busy) { canIngest = it }
            PermissionRow("研究（使用已配置的外部工具）", canResearch, !busy) { canResearch = it }
            PermissionRow("整理 / 命名", canOrganize, !busy) { canOrganize = it }
            if (writesNeedLibrary) {
                Text("允许写入的影库 · ${libraryIds.size}/100", style = MaterialTheme.typography.titleSmall)
                libraries.forEach { item -> PermissionRow(item.name, item.id in libraryIds, !busy) { selected -> libraryIds = if (selected) libraryIds + item.id else libraryIds - item.id } }
            }
            Text("作者资料权限", style = MaterialTheme.typography.titleSmall)
            listOf("name", "biography", "homepage", "aliases").forEach { field -> PermissionRow(fieldName(field), field in fields, !busy) {
                fields = if (it) fields + field else fields - field
            } }
            listOf("x", "basic", "accounts").forEach { block -> PermissionRow("提交${fieldName(block)}候选", block in blocks, !busy) { blocks = if (it) blocks + block else blocks - block } }
            if (writesNeedAuthor) {
                PermissionRow("全部可维护作者", allAuthors, !busy) { allAuthors = it }
                if (!allAuthors) {
                    Text("指定作者 · ${authorIds.size}/100")
                    authors.items.filter { it.kind == "regular" }.forEach { author -> PermissionRow(author.name, author.id in authorIds, !busy) { selected -> authorIds = if (selected) authorIds + author.id else authorIds - author.id } }
                    if (authors.cursor != null) TextButton(onClick = { library.loadAuthors(true) }, enabled = !authors.loading) { Text("更多作者") }
                    authors.error?.let { ErrorNotice(it) { library.loadAuthors() } }
                }
                PermissionRow("允许清空已授权字段", allowClear, !busy) { allowClear = it }
            }
            OutlinedTextField(days, { days = it }, label = { Text("有效天数（1–365）") }, isError = validDays == null, enabled = !busy)
            OutlinedTextField(tasks, { tasks = it }, label = { Text("每日任务上限（1–200）") }, isError = dailyTasks == null, enabled = !busy)
            OutlinedTextField(updates, { updates = it }, label = { Text("每任务资料更新上限（1–10）") }, isError = maxUpdates == null, enabled = !busy)
            if (canResearch) OutlinedTextField(research, { research = it }, label = { Text("每任务研究调用上限（0–100）") }, isError = maxResearch == null, enabled = !busy)
            OutlinedTextField(calls, { calls = it }, label = { Text("每任务工具调用上限（10–10000）") }, isError = maxCalls == null, enabled = !busy)
            OutlinedTextField(minutes, { minutes = it }, label = { Text("单任务有效分钟数（1–1440）") }, isError = duration == null, enabled = !busy)
            error?.let { ErrorNotice(it) }
        }
    }, confirmButton = { TextButton(onClick = {
        model.create(AIAccessPolicy(label.trim(), libraryIds, if (allAuthors) emptyList() else authorIds, allAuthors,
            fields, blocks, allowClear && writesNeedAuthor, canIngest, canResearch, canOrganize,
            if (canResearch) maxResearch!! else 0, maxUpdates!!, dailyTasks!!, validDays!!, maxCalls!!, duration!! * 60), close)
    }, enabled = valid && !busy && error == null) { Text("创建接入凭据") } },
        dismissButton = { TextButton(onClick = close, enabled = !busy) { Text("取消") } })
}

@Composable
private fun PermissionRow(label: String, selected: Boolean, enabled: Boolean, changed: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) { Checkbox(selected, changed, enabled = enabled); Text(label, Modifier.weight(1f)) }
}

@Composable
private fun OneTimeCredential(token: String, close: () -> Unit) {
    val context = LocalContext.current
    var copied by remember { mutableStateOf(false) }
    var copyError by remember { mutableStateOf(false) }
    AlertDialog(onDismissRequest = close, title = { Text("保存 AI 接入凭据") }, properties = DialogProperties(securePolicy = SecureFlagPolicy.SecureOn), text = {
        Column(Modifier.heightIn(max = 420.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("仅此次展示。复制到你配置的可信工具中；关闭后本应用不保留凭据。")
            Text(token, fontFamily = FontFamily.Monospace)
            if (copyError) Text("复制失败，请检查系统剪贴板限制。", color = MaterialTheme.colorScheme.error)
        }
    }, confirmButton = { TextButton(onClick = {
        val clip = ClipData.newPlainText("AI 接入凭据", token)
        if (Build.VERSION.SDK_INT >= 33) clip.description.extras = PersistableBundle().apply { putBoolean(ClipDescription.EXTRA_IS_SENSITIVE, true) }
        try {
            (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(clip)
            copied = true; copyError = false
        } catch (_: SecurityException) { copyError = true }
    }) { Text(if (copied) "已复制" else "复制凭据") } }, dismissButton = { TextButton(onClick = close) { Text("已保存，关闭") } })
}

private fun fieldName(value: String) = mapOf("name" to "名称", "biography" to "简介", "homepage" to "主页", "aliases" to "别名",
    "x" to "X 资料", "basic" to "基本资料", "accounts" to "账号资料")[value] ?: value
