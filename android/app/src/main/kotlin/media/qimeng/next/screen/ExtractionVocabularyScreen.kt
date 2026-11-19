package media.qimeng.next.screen

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import media.qimeng.next.ExtractionVocabularyViewModel
import media.qimeng.next.model.ExtractionGroup
import media.qimeng.next.model.ExtractionRole
import media.qimeng.next.model.ExtractionVocabulary
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.QimengCapsuleTextField
import media.qimeng.next.ui.component.QimengSettingsCard
import media.qimeng.next.ui.component.QimengTopBar

private const val TEXT_LIMIT = 100
private const val GROUP_LIMIT = 256
private const val WORD_LIMIT = 256
private const val VARIANT_LIMIT = 64
private const val ROLE_LIMIT = 128
private const val ALIAS_LIMIT = 32
private data class ExtractionPrompt(val title: String, val initial: String = "", val apply: (String) -> Unit)

/** The old VocabularyEditScreen's stop words, expandable groups and one-field prompts. */
@Composable
internal fun ExtractionVocabularyScreen(
    target: Session,
    transport: Long,
    back: () -> Unit,
    model: ExtractionVocabularyViewModel = viewModel(),
) {
    val current by model.state.collectAsStateWithLifecycle()
    val state = current.takeIf { sameTarget(target, it.target) }
    val draft = state?.draft
    val saving = state?.saving == true
    var groupIndex by rememberSaveable(target.instanceId, target.baseUrl) { mutableIntStateOf(-1) }
    var prompt by remember(target) { mutableStateOf<ExtractionPrompt?>(null) }
    var reload by remember(target) { mutableStateOf(false) }
    var discard by remember(target) { mutableStateOf(false) }
    val requestBack: () -> Unit = {
        if (!saving) {
            if (state?.dirty == true) discard = true else back()
        }
    }
    LaunchedEffect(target, transport) { model.open(target, transport) }
    val owner = LocalLifecycleOwner.current
    val poll = state != null && state.base?.rebuilding == true && !state.conflict && state.error == null
    LaunchedEffect(owner, target, transport, poll) {
        if (poll) owner.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (isActive) {
                delay(1_500)
                model.refresh()
            }
        }
    }
    BackHandler(onBack = requestBack)

    Column(Modifier.fillMaxSize()) {
        QimengTopBar("原名提取词表", onBack = requestBack)
        LazyColumn(Modifier.fillMaxSize().imePadding().testTag("extraction-vocabulary-list"),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp)) {
            item {
                Text("${target.label} · 编辑出处组、角色别名和停用词。内置词表始终生效。", style = MaterialTheme.typography.bodySmall)
                state?.error?.let { ErrorNotice(it) { model.refresh() } }
                if (state?.conflict == true) {
                    ErrorNotice("服务端词表已被修改。本次草稿仍保留。")
                    TextButton(onClick = { reload = true }, enabled = !state.loading) { Text("读取服务端新词表") }
                }
                state?.base?.let { RebuildStatus(it, state.saved) }
                if (draft == null && state?.error == null) Text("正在读取词表…", style = MaterialTheme.typography.bodySmall)
            }
            if (state != null && draft != null) {
                item {
                    ExtractionCard {
                        TermList("停用词", "添加停用词", draft.stopWords, !saving, WORD_LIMIT,
                            { prompt = ExtractionPrompt("新增停用词") { word ->
                                model.edit { it.copy(stopWords = (it.stopWords + word).distinct()) }
                            } },
                            { index -> model.edit { it.copy(stopWords = it.stopWords.without(index)) } })
                        if (draft.stopWords.isEmpty()) Text("没有自定义停用词", style = MaterialTheme.typography.bodySmall)
                    }
                }
                item {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("出处组", Modifier.weight(1f), style = MaterialTheme.typography.titleSmall)
                        TextButton(onClick = {
                            prompt = ExtractionPrompt("新增出处组（规范名）") { name ->
                                model.edit {
                                    require(it.groups.none { group -> group.canonical == name }) { "已有同名出处组。" }
                                    it.copy(groups = it.groups + ExtractionGroup(name))
                                }
                            }
                        }, enabled = !saving && draft.groups.size < GROUP_LIMIT) { Text("添加出处组") }
                    }
                    if (draft.groups.isEmpty()) Text("没有自定义出处组", style = MaterialTheme.typography.bodySmall)
                }
                itemsIndexed(draft.groups, key = { _, group -> group.canonical }) { index, group ->
                    ExtractionGroupCard(group, groupIndex == index, !saving,
                        onExpand = { groupIndex = if (groupIndex == index) -1 else index },
                        onAsk = { prompt = it },
                        onRemove = {
                            groupIndex = -1
                            model.edit { it.copy(groups = it.groups.without(index)) }
                        },
                        onEdit = { change -> model.edit {
                            val updated = change(it.groups[index])
                            require(it.groups.indices.none { position -> position != index && it.groups[position].canonical == updated.canonical }) { "已有同名出处组。" }
                            it.copy(groups = it.groups.replacing(index, updated))
                        } },
                    )
                }
                item {
                    val failed = state.base?.rebuildState == "failed"
                    Button(onClick = model::save, enabled = (state.dirty || failed) && !saving && !state.loading && !state.conflict,
                        modifier = Modifier.fillMaxWidth()) {
                        Text(if (saving) "保存中…" else if (failed && !state.dirty) "重试重算" else "保存修改")
                    }
                    Text("保存后由服务器后台重算。明确填写或清空的媒体归属会保留。", style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }
    prompt?.let { ExtractionPromptDialog(it) { prompt = null } }
    if (reload || discard) AlertDialog(
        onDismissRequest = { reload = false; discard = false },
        title = { Text("放弃未保存的修改？") },
        text = { Text(if (reload) "确认后读取最新词表。读取成功后，本次草稿将被替换。" else "离开后将丢失本次编辑。") },
        confirmButton = { TextButton(onClick = {
            if (reload) model.refresh(replaceDraft = true) else { model.discard(); back() }
            reload = false
            discard = false
        }) { Text(if (reload) "放弃并读取" else "放弃修改离开") } },
        dismissButton = { TextButton(onClick = { reload = false; discard = false }) { Text("继续编辑") } },
    )
}

@Composable
private fun RebuildStatus(base: ExtractionVocabulary, saved: Boolean) {
    val title = when (base.rebuildState) {
        "pending" -> "服务端等待重算"
        "running" -> "服务端正在重算"
        "completed" -> "服务端重算完成"
        "failed" -> "服务端重算失败"
        else -> "服务端重算状态未知"
    }
    Text("${if (saved) "已保存 · " else ""}$title · 修订 ${base.revision}", Modifier.testTag("extraction-rebuild-state"),
        style = MaterialTheme.typography.bodySmall)
    Text("处理 ${base.processed} 项 · 匹配 ${base.matched} 项 · 待核对 ${base.ambiguous} 项", style = MaterialTheme.typography.bodySmall)
    if (base.rebuildState == "failed") Text("可重新提交当前词表以重试重算。", style = MaterialTheme.typography.bodySmall)
}

@Composable
private fun ExtractionGroupCard(
    group: ExtractionGroup,
    expanded: Boolean,
    enabled: Boolean,
    onExpand: () -> Unit,
    onAsk: (ExtractionPrompt) -> Unit,
    onRemove: () -> Unit,
    onEdit: ((ExtractionGroup) -> ExtractionGroup) -> Unit,
) {
    var activeRole by rememberSaveable(group.canonical) { mutableIntStateOf(-1) }
    ExtractionCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(group.canonical, style = MaterialTheme.typography.titleSmall)
                Text("${group.variants.size} 个变体 · ${group.roles.size} 个角色", style = MaterialTheme.typography.bodySmall)
            }
            TextButton(onClick = onExpand, modifier = Modifier.testTag("extraction-group:${group.canonical}")) {
                Text(if (expanded) "收起" else "展开")
            }
        }
        if (expanded) {
            Row {
                TextButton(onClick = { onAsk(ExtractionPrompt("修改组规范名", group.canonical) { name -> onEdit {
                    if (name == it.canonical) it else it.copy(canonical = name,
                        variants = retainSpelling(it.variants, it.canonical, name, VARIANT_LIMIT))
                } }) }, enabled = enabled) { Text("修改组名") }
                TextButton(onClick = onRemove, enabled = enabled) { Text("移除组") }
            }
            TermList("变体写法", "添加变体", group.variants, enabled, VARIANT_LIMIT,
                { onAsk(ExtractionPrompt("新增变体写法") { name -> onEdit { it.copy(variants = (it.variants + name).distinct()) } }) },
                { index -> onEdit { it.copy(variants = it.variants.without(index)) } })
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("角色", Modifier.weight(1f), style = MaterialTheme.typography.titleSmall)
                TextButton(onClick = { onAsk(ExtractionPrompt("新增角色（规范名）") { name -> onEdit {
                    require(it.roles.none { role -> role.canonical == name }) { "该组已有同名角色。" }
                    it.copy(roles = it.roles + ExtractionRole(name))
                } }) }, enabled = enabled && group.roles.size < ROLE_LIMIT) { Text("添加角色") }
            }
            group.roles.forEachIndexed { index, role ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(role.canonical, Modifier.weight(1f))
                    TextButton(onClick = { activeRole = if (activeRole == index) -1 else index }) { Text(if (activeRole == index) "收起角色" else "角色别名") }
                }
                if (activeRole == index) {
                    Row {
                        TextButton(onClick = { onAsk(ExtractionPrompt("修改角色规范名", role.canonical) { name -> onEdit {
                            require(it.roles.indices.none { i -> i != index && it.roles[i].canonical == name }) { "该组已有同名角色。" }
                            if (name == role.canonical) it else it.copy(roles = it.roles.replacing(index,
                                ExtractionRole(name, retainSpelling(role.aliases, role.canonical, name, ALIAS_LIMIT))))
                        } }) }, enabled = enabled) { Text("修改角色名") }
                        TextButton(onClick = {
                            activeRole = -1
                            onEdit { it.copy(roles = it.roles.without(index)) }
                        }, enabled = enabled) { Text("移除角色") }
                    }
                    TermList("别名", "添加别名", role.aliases, enabled, ALIAS_LIMIT,
                        { onAsk(ExtractionPrompt("新增角色别名") { name -> onEdit {
                            it.copy(roles = it.roles.replacing(index, role.copy(aliases = (role.aliases + name).distinct())))
                        } }) },
                        { alias -> onEdit { it.copy(roles = it.roles.replacing(index, role.copy(aliases = role.aliases.without(alias)))) } })
                }
            }
        }
    }
}

@Composable
private fun ExtractionCard(content: @Composable ColumnScope.() -> Unit) {
    QimengSettingsCard {
        Column(Modifier.fillMaxWidth().padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp), content = content)
    }
}

@Composable
private fun TermList(title: String, add: String, terms: List<String>, enabled: Boolean, limit: Int, onAdd: () -> Unit, onRemove: (Int) -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(title, Modifier.weight(1f), style = MaterialTheme.typography.titleSmall)
        TextButton(onClick = onAdd, enabled = enabled && terms.size < limit) { Text(add) }
    }
    terms.forEachIndexed { index, term ->
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(term, Modifier.weight(1f))
            TextButton(onClick = { onRemove(index) }, enabled = enabled) { Text("移除") }
        }
    }
}

@Composable
private fun ExtractionPromptDialog(prompt: ExtractionPrompt, dismiss: () -> Unit) {
    var text by rememberSaveable(prompt.title, prompt.initial) { mutableStateOf(prompt.initial) }
    val count = text.codePointCount(0, text.length)
    AlertDialog(onDismissRequest = dismiss, containerColor = MaterialTheme.colorScheme.surface, title = { Text(prompt.title) },
        text = {
            Column {
                QimengCapsuleTextField(text, { text = it }, placeholder = "输入词条", singleLine = true,
                    modifier = Modifier.fillMaxWidth().testTag("extraction-term"))
                Text("$count/$TEXT_LIMIT", style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = { TextButton(onClick = { prompt.apply(text.trim()); dismiss() },
            enabled = text.isNotBlank() && count <= TEXT_LIMIT && text.none(Char::isISOControl)) { Text("确定") } },
        dismissButton = { TextButton(onClick = dismiss) { Text("取消") } },
    )
}

private fun retainSpelling(spellings: List<String>, old: String, current: String, limit: Int): List<String> {
    val result = (spellings.filterNot { it == current } + old).distinct()
    require(result.size <= limit) { "请先移除一个现有写法，为旧名称留出位置。" }
    return result
}

private fun <T> List<T>.without(index: Int): List<T> = toMutableList().apply { removeAt(index) }
private fun <T> List<T>.replacing(index: Int, value: T): List<T> = toMutableList().apply { set(index, value) }
