package media.qimeng.next.screen

import android.content.ClipData
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.os.Build
import android.os.PersistableBundle
import androidx.activity.compose.BackHandler
import androidx.activity.compose.LocalActivity
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.DialogProperties
import androidx.compose.ui.window.SecureFlagPolicy
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.AIAccessViewModel
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.AIAccessGrant
import media.qimeng.next.model.AIAccessPolicy
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice

@Composable
@OptIn(ExperimentalLayoutApi::class)
fun AIAccessScreen(library: LibraryViewModel, model: AIAccessViewModel = viewModel()) {
    val state by model.state.collectAsStateWithLifecycle()
    val filter by model.filter.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val secret by model.secret.collectAsStateWithLifecycle()
    val reconcile by model.reconcileRequired.collectAsStateWithLifecycle()
    val activity = LocalActivity.current
    var creating by rememberSaveable { mutableStateOf(false) }
    var showConfig by rememberSaveable { mutableStateOf(false) }
    var revoking by rememberSaveable { mutableStateOf<String?>(null) }
    BackHandler(busy) { }
    LaunchedEffect(Unit) { if (!state.loaded) model.refresh() }
    LaunchedEffect(secret) { if (secret != null) creating = false }
    DisposableEffect(model) { onDispose { if (activity?.isChangingConfigurations != true) model.dismissSecret() } }
    Column(Modifier.fillMaxSize()) {
        PageHeader("AI 接入", library::back, backEnabled = !busy)
        LazyColumn(Modifier.testTag("ai-access-records"), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            item { ContentCard {
                Text("连接你使用的 AI 工具", style = MaterialTheme.typography.titleMedium)
                Text("默认只读可查询全库。影库与作者选择限制写入范围，不限制读取。正常授权任务由工具自动接续，无需逐批复制任务凭据。")
                Row {
                    Button(onClick = { model.clearCreationError(); creating = true }, enabled = !busy && !reconcile) { Text("新增 AI 接入") }
                    TextButton(onClick = { model.refresh() }, enabled = !busy) { Text("刷新") }
                }
                TextButton(onClick = { showConfig = !showConfig }) { Text(if (showConfig) "收起执行器配置" else "核对执行器配置") }
                if (showConfig) {
                    Text("在聊天工具的 MCP 配置中选择 stdio，启动命令为 qai mcp。")
                    Text("QIMENG_API_BASE · 影库服务器地址\nQIMENG_API_TOKEN · 本次签发的凭据\nQIMENG_AI_STATE_DIR · 执行器的私有状态目录\nQIMENG_AI_PERMISSIONS · 允许执行器使用的权限",
                        style = MaterialTheme.typography.bodySmall)
                }
            } }
            item { FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf("active" to "有效", "expired" to "过期", "revoked" to "撤销", "all" to "全部").forEach { (key, label) ->
                    FilterChip(filter == key, { model.refresh(state = key) }, enabled = !busy, label = { Text(label) })
                }
            } }
            if (state.loading || busy) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
            if (reconcile) item { ErrorNotice("上次创建结果未确认，请刷新有效连接核对后再继续。") { model.refresh(state = "active") } }
            state.error?.let { item { ErrorNotice(it) { model.refresh() } } }
            if (state.loaded && state.items.isEmpty()) item { Text("此状态暂无 AI 接入") }
            items(state.items, key = { it.id }) { grant -> GrantCard(grant, !busy) { revoking = grant.id } }
            if (state.cursor != null) item { TextButton(onClick = { model.refresh(next = true) }, enabled = !busy && !state.loading) { Text("加载更多接入") } }
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
private fun GrantCard(grant: AIAccessGrant, enabled: Boolean, revoke: () -> Unit) {
    var expanded by rememberSaveable(grant.id) { mutableStateOf(false) }
    val status = when (grant.state) {
        "active" -> "有效"
        "revoked" -> "已撤销"
        "expired" -> "已过期"
        else -> "状态待核对"
    }
    ContentCard(Modifier.testTag("ai-grant:${grant.id}")) {
        Text(grant.policy.label, style = MaterialTheme.typography.titleMedium)
        Text("$status · ${grant.policy.actions(grant.canLookupImages)}")
        Text("建立于 ${grant.createdAt}\n到期于 ${grant.expiresAt}", style = MaterialTheme.typography.bodySmall)
        TextButton(onClick = { expanded = !expanded }) { Text(if (expanded) "收起权限详情" else "权限详情") }
        if (expanded) PolicyDetails(grant.policy, grant.canLookupImages)
        if (grant.state == "active") TextButton(onClick = revoke, enabled = enabled) { Text("撤销接入") }
    }
}

private fun AIAccessPolicy.actions(canLookupImages: Boolean) = buildList {
    if (canIngest) add("新增媒体")
    if (canOrganize) add("整理与命名")
    if (canRecycleSources) add("备份核验后回收来源")
    if (canResearch || canResearchAssets || canLookupImages) add("研究")
    if (candidateBlocks.isNotEmpty()) add("提交候选")
    if (allowedFields.isNotEmpty()) add("维护作者")
    if (assetFields.isNotEmpty()) add("维护作品")
}.joinToString("、").ifEmpty { "仅全库读取" }

@Composable
private fun PolicyDetails(policy: AIAccessPolicy, canLookupImages: Boolean = false,
    libraryNames: Map<String, String> = emptyMap(), authorNames: Map<String, String> = emptyMap()) {
    fun List<String>.names(names: Map<String, String>) = joinToString("、") { names[it] ?: it }.ifEmpty { "未选择" }
    val detail = MaterialTheme.typography.bodySmall
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text("读取范围 · 全库资料", style = detail)
        Text("执行动作 · ${policy.actions(canLookupImages)}", style = detail)
        Text("授权影库 · ${policy.libraryIds.names(libraryNames)}", style = detail)
        Text("研究 · ${if (policy.canResearch) "已授权" else "未授权"}", style = detail)
        Text("已有作品预览研究 · ${if (policy.canResearchAssets) "已授权" else "未授权"}", style = detail)
        Text("作品维护 · ${policy.assetFields.joinToString("、", transform = ::assetFieldName).ifEmpty { "未授权" }}", style = detail)
        Text("公开文字查询来源 · ${policy.researchProviders.joinToString("、") { researchSourceNames[it] ?: it }.ifEmpty { "未授权" }}", style = detail)
        Text("单帧识图 · ${if (canLookupImages) "允许向已配置的识图服务发送一帧派生预览" else "未授权"}", style = detail)
        Text("作者范围 · ${if (policy.allAuthors) "全部可维护作者（含今后新增）" else policy.authorIds.names(authorNames)}", style = detail)
        Text("待核对的资料候选 · ${policy.candidateBlocks.joinToString("、", transform = ::fieldName).ifEmpty { "未授权" }}", style = detail)
        Text("直接维护作者字段 · ${policy.allowedFields.joinToString("、", transform = ::fieldName).ifEmpty { "未授权" }}", style = detail)
        Text("清空已授权字段 · ${if (policy.allowClear) "允许" else "未授权"}", style = detail)
        Text("授权有效期 · ${policy.validDays} 天", style = detail)
        Text("每日最多 ${policy.maxTasksPerDay} 个任务，每任务最多 ${policy.maxCallsPerTask} 次工具调用，有效 ${policy.taskSeconds} 秒", style = detail)
        Text("每任务最多 ${policy.maxUpdates} 次资料更新、${policy.maxResearchCalls} 次研究调用", style = detail)
    }
}

@Composable
private fun AIAccessForm(library: LibraryViewModel, model: AIAccessViewModel, close: () -> Unit) {
    val libraries by library.libraries.collectAsStateWithLifecycle()
    val libraryPage by library.libraryPage.collectAsStateWithLifecycle()
    val authors by library.authors.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val error by model.creationError.collectAsStateWithLifecycle()
    val sources by model.researchSources.collectAsStateWithLifecycle()
    var label by rememberSaveable { mutableStateOf("我的 AI 助手") }
    var libraryIds by rememberSaveable { mutableStateOf(emptyList<String>()) }
    var authorIds by rememberSaveable { mutableStateOf(emptyList<String>()) }
    var allAuthors by rememberSaveable { mutableStateOf(false) }
    var fields by rememberSaveable { mutableStateOf(emptyList<String>()) }
    var blocks by rememberSaveable { mutableStateOf(emptyList<String>()) }
    var canIngest by rememberSaveable { mutableStateOf(false) }
    var canResearch by rememberSaveable { mutableStateOf(false) }
    var researchProviders by rememberSaveable { mutableStateOf(emptyList<String>()) }
    var canOrganize by rememberSaveable { mutableStateOf(false) }
    var canRecycle by rememberSaveable { mutableStateOf(false) }
    var researchAssets by rememberSaveable { mutableStateOf(false) }
    var assetFields by rememberSaveable { mutableStateOf(emptyList<String>()) }
    var allowClear by rememberSaveable { mutableStateOf(false) }
    var days by rememberSaveable { mutableStateOf("30") }
    var tasks by rememberSaveable { mutableStateOf("20") }
    var updates by rememberSaveable { mutableStateOf("1") }
    var research by rememberSaveable { mutableStateOf("8") }
    var calls by rememberSaveable { mutableStateOf("100") }
    var minutes by rememberSaveable { mutableStateOf("60") }
    LaunchedEffect(Unit) { library.refreshConnection(); library.loadAuthors() }
    LaunchedEffect(canResearch) { if (canResearch) model.loadResearchSources() }
    val validDays = days.toIntOrNull()?.takeIf { it in 1..365 }
    val dailyTasks = tasks.toIntOrNull()?.takeIf { it in 1..200 }
    val maxUpdates = updates.toIntOrNull()?.takeIf { it in 1..10 }
    val maxResearch = if (canResearch) research.toIntOrNull()?.takeIf { it in 0..100 } else 0
    val maxCalls = calls.toIntOrNull()?.takeIf { it in 10..10000 }
    val duration = minutes.toIntOrNull()?.takeIf { it in 1..1440 }
    LaunchedEffect(label, libraryIds, authorIds, allAuthors, fields, blocks, canIngest, canResearch, researchProviders, canOrganize, canRecycle, researchAssets, assetFields, allowClear, days, tasks, updates, research, calls, minutes) {
        model.clearCreationError()
    }
    var step by rememberSaveable { mutableIntStateOf(0) }
    val needsLibrary = canIngest || canOrganize || researchAssets || assetFields.isNotEmpty()
    val needsAuthor = fields.isNotEmpty() || blocks.isNotEmpty()
    val showLibraries = needsLibrary || canResearch
    val nameValid = label.isNotBlank() && label.trim().encodeToByteArray().size <= 80
    val scopesValid = (!needsLibrary || libraryIds.isNotEmpty()) && (!needsAuthor || allAuthors || authorIds.isNotEmpty()) &&
        libraryIds.size <= 100 && authorIds.size <= 100 &&
        (researchProviders.isEmpty() || sources.data?.providers?.containsAll(researchProviders) == true)
    val policy = if (nameValid && scopesValid && validDays != null && dailyTasks != null && maxUpdates != null &&
        maxResearch != null && maxCalls != null && duration != null) {
        AIAccessPolicy(
            label = label.trim(), libraryIds = if (showLibraries) libraryIds else emptyList(),
            authorIds = if (needsAuthor && !allAuthors) authorIds else emptyList(), allAuthors = needsAuthor && allAuthors,
            allowedFields = fields, candidateBlocks = blocks, allowClear = allowClear && fields.isNotEmpty(),
            canIngest = canIngest, canResearch = canResearch, canOrganize = canOrganize, canRecycleSources = canRecycle,
            canResearchAssets = researchAssets, assetFields = assetFields, researchProviders = researchProviders,
            maxResearchCalls = maxResearch, maxUpdates = maxUpdates, maxTasksPerDay = dailyTasks,
            validDays = validDays, maxCallsPerTask = maxCalls, taskSeconds = duration * 60,
        )
    } else null
    AlertDialog(
        onDismissRequest = { if (!busy) close() },
        title = { Text("新增 AI 接入") },
        text = {
            Column(Modifier.heightIn(max = 520.dp).verticalScroll(key(step) { rememberScrollState() }),
                verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("第 ${step + 1} 步 · ${listOf("选择动作", "范围与预算", "核对并建立")[step]}", style = MaterialTheme.typography.titleMedium)
                when (step) {
                    0 -> {
                        OutlinedTextField(label, { label = it }, label = { Text("连接名称") }, enabled = !busy, isError = !nameValid)
                        Text("默认仅可读取全库资料。请勾选执行器需要的动作。", style = MaterialTheme.typography.bodySmall)
                        PermissionRow("新增媒体", canIngest, !busy) { canIngest = it; if (!it) canRecycle = false }
                        PermissionRow("整理 / 命名", canOrganize, !busy) { canOrganize = it }
                        PermissionRow("备份核验后回收来源", canRecycle, !busy && canIngest) { canRecycle = it }
                        PermissionRow("研究", canResearch, !busy) { enabled ->
                            canResearch = enabled
                            if (!enabled) { researchAssets = false; researchProviders = emptyList(); blocks = emptyList() }
                        }
                        Text("现有作品权限", style = MaterialTheme.typography.titleSmall)
                        PermissionRow("研究现有作品", researchAssets, !busy && canResearch) { researchAssets = it }
                        listOf("authors", "taxonomy", "tags", "rename").forEach { field ->
                            PermissionRow(assetFieldName(field), field in assetFields, !busy) { checked ->
                                assetFields = if (checked) assetFields + field else assetFields - field
                            }
                        }
                        Text("作者资料候选", style = MaterialTheme.typography.titleSmall)
                        Text("开启研究后可提交候选。经你核对并保存后，候选才会进入已确认资料。", style = MaterialTheme.typography.bodySmall)
                        listOf("x", "basic", "accounts").forEach { block ->
                            PermissionRow("提交${fieldName(block)}候选", block in blocks, !busy && canResearch) { checked ->
                                blocks = if (checked) blocks + block else blocks - block
                            }
                        }
                        Text("直接维护作者字段", style = MaterialTheme.typography.titleSmall)
                        listOf("name", "biography", "homepage", "aliases").forEach { field ->
                            PermissionRow(fieldName(field), field in fields, !busy) { checked ->
                                fields = if (checked) fields + field else fields - field
                                if (fields.isEmpty()) allowClear = false
                            }
                        }
                        PermissionRow("允许清空已授权字段", allowClear, !busy && fields.isNotEmpty()) { allowClear = it }
                    }
                    1 -> {
                        if (showLibraries) {
                            Text("授权影库 · ${libraryIds.size}/100", style = MaterialTheme.typography.titleSmall)
                            if (needsLibrary && libraryIds.isEmpty()) Text("请为所选动作指定至少一个影库。")
                            libraries.forEach { item -> PermissionRow(item.name, item.id in libraryIds, !busy) { checked ->
                                libraryIds = if (checked) libraryIds + item.id else libraryIds - item.id
                            } }
                            if (libraryPage.cursor != null) TextButton(onClick = { library.loadLibraries(next = true) }, enabled = !libraryPage.loading) { Text("更多媒体库") }
                            libraryPage.error?.let { ErrorNotice(it) { library.loadLibraries(next = libraryPage.cursor != null) } }
                        }
                        if (needsAuthor) {
                            PermissionRow("全部可维护作者", allAuthors, !busy) { allAuthors = it }
                            if (!allAuthors) {
                                Text("指定作者 · ${authorIds.size}/100")
                                authors.items.filter { it.kind == "regular" }.forEach { author ->
                                    PermissionRow(author.name, author.id in authorIds, !busy) { checked ->
                                        authorIds = if (checked) authorIds + author.id else authorIds - author.id
                                    }
                                }
                                if (authors.cursor != null) TextButton(onClick = { library.loadAuthors(true) }, enabled = !authors.loading) { Text("更多作者") }
                                authors.error?.let { ErrorNotice(it) { library.loadAuthors() } }
                            }
                        }
                        if (canResearch) {
                            Text("外部研究来源", style = MaterialTheme.typography.titleSmall)
                            Text("所选来源可收到任务明确提交的公开标识或检索词。", style = MaterialTheme.typography.bodySmall)
                            if (sources.loading) LinearProgressIndicator(Modifier.fillMaxWidth())
                            sources.error?.let { ErrorNotice(it, model::loadResearchSources) }
                            if (sources.data?.enabled == false) Text("服务器当前已关闭外部查询。所选来源会在服务开启后生效。", style = MaterialTheme.typography.bodySmall)
                            sources.data?.providers.orEmpty().forEach { name ->
                                PermissionRow(researchSourceNames[name] ?: name, name in researchProviders, !busy && (maxResearch ?: 0) > 0) { checked ->
                                    researchProviders = if (checked) researchProviders + name else researchProviders - name
                                }
                            }
                        }
                        OutlinedTextField(days, { days = it }, label = { Text("有效天数（1–365）") }, isError = validDays == null, enabled = !busy)
                        OutlinedTextField(tasks, { tasks = it }, label = { Text("每日任务上限（1–200）") }, isError = dailyTasks == null, enabled = !busy)
                        OutlinedTextField(updates, { updates = it }, label = { Text("每任务资料更新上限（1–10）") }, isError = maxUpdates == null, enabled = !busy)
                        if (canResearch) OutlinedTextField(research, { research = it; if (it.toIntOrNull() == 0) researchProviders = emptyList() },
                            label = { Text("每任务研究调用上限（0–100）") }, isError = maxResearch == null, enabled = !busy)
                        OutlinedTextField(calls, { calls = it }, label = { Text("每任务工具调用上限（10–10000）") }, isError = maxCalls == null, enabled = !busy)
                        OutlinedTextField(minutes, { minutes = it }, label = { Text("单任务有效分钟数（1–1440）") }, isError = duration == null, enabled = !busy)
                    }
                    else -> if (policy == null) {
                        ErrorNotice("请返回上一步核对范围和预算。")
                    } else {
                        Text(policy.label, style = MaterialTheme.typography.titleSmall)
                        PolicyDetails(policy, libraryNames = libraries.associate { it.id to it.name },
                            authorNames = authors.items.associate { it.id to it.name })
                    }
                }
                error?.let { ErrorNotice(it) }
            }
        },
        confirmButton = { TextButton(
            onClick = { if (step < 2) step++ else if (policy != null) model.create(policy, close) },
            enabled = !busy && if (step == 0) nameValid else policy != null && (step < 2 || error == null),
        ) { Text(if (step < 2) "下一步" else "创建接入凭据") } },
        dismissButton = {
            Row {
                if (step > 0) TextButton(onClick = { step-- }, enabled = !busy) { Text("上一步") }
                TextButton(onClick = close, enabled = !busy) { Text("取消") }
            }
        },
    )
}

private val researchSourceNames = mapOf("fxtwitter" to "FxTwitter", "pixiv" to "Pixiv", "yandere" to "yande.re",
    "e621" to "e621", "wayback" to "Wayback Machine", "steam" to "Steam", "bing" to "Bing")

@Composable
private fun PermissionRow(label: String, selected: Boolean, enabled: Boolean, changed: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().heightIn(min = 48.dp).toggleable(
        value = selected, enabled = enabled, role = Role.Checkbox, onValueChange = changed,
    ),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Checkbox(selected, onCheckedChange = null, enabled = enabled)
        Text(label, Modifier.weight(1f))
    }
}

@Composable
private fun OneTimeCredential(token: String, close: () -> Unit) {
    val context = LocalContext.current
    var revealed by remember(token) { mutableStateOf(false) }
    var copied by remember(token) { mutableStateOf(false) }
    var copyError by remember(token) { mutableStateOf(false) }
    AlertDialog(onDismissRequest = close, title = { Text("保存 AI 接入凭据") }, properties = DialogProperties(securePolicy = SecureFlagPolicy.SecureOn), text = {
        Column(Modifier.heightIn(max = 420.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("凭据仅可在这里取用。请保存到可信执行器的 QIMENG_API_TOKEN，关闭后无法再次读取。")
            Text(if (revealed) token else "•••• •••• •••• ••••", fontFamily = FontFamily.Monospace)
            TextButton(onClick = { revealed = !revealed }) { Text(if (revealed) "隐藏凭据" else "显示凭据") }
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

private fun assetFieldName(value: String) = when (value) {
    "authors" -> "修改作品作者"; "taxonomy" -> "修改作品出处与角色"; "tags" -> "修改作品标签"; "rename" -> "作品改名"; else -> value
}
