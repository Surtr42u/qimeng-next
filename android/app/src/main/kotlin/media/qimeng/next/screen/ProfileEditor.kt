package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.compose.ui.window.DialogWindowProvider
import androidx.core.view.WindowCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.AuthorProfileViewModel
import media.qimeng.next.model.*
import media.qimeng.next.ui.component.QimengSegPill
import media.qimeng.next.ui.component.QimengTopBar
import media.qimeng.next.ui.theme.QimengDimens

private const val BIOGRAPHY_LIMIT = 20_000

private val accountSaver = listSaver<List<ProfileAccount>, String>(
    save = { accounts -> accounts.flatMap { listOf(it.platform, it.identifier, it.url, it.publishingUrl) } },
    restore = { values -> values.chunked(4).map { ProfileAccount(it[0], it[1], it[2], it[3]) } })

@Composable
internal fun ProfileEditor(authorName: String, document: ProfileDocument, block: ProfileBlock, candidate: ProfileCandidate?, model: AuthorProfileViewModel, onClose: () -> Unit) {
    key(document.target.baseUrl, document.target.instanceId, document.authorId, block.block, candidate?.id) {
        ProfileEditorContent(authorName, document, block, candidate, model, onClose)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ProfileEditorContent(authorName: String, document: ProfileDocument, block: ProfileBlock, candidate: ProfileCandidate?, model: AuthorProfileViewModel, onClose: () -> Unit) {
    val busy by model.busy.collectAsStateWithLifecycle()
    val error by model.error.collectAsStateWithLifecycle()
    val initial = remember { profileDraftValues(block, candidate) }
    val baseRevision = rememberSaveable { block.revision }
    val originalBaseline = rememberSaveable { block.values.biographyOriginal.orEmpty() }
    var handle by rememberSaveable { mutableStateOf(initial.handle.orEmpty()) }
    var displayName by rememberSaveable { mutableStateOf(initial.displayName.orEmpty()) }
    var profileUrl by rememberSaveable { mutableStateOf(initial.profileUrl.orEmpty()) }
    var original by rememberSaveable { mutableStateOf(initial.biographyOriginal.orEmpty()) }
    var translation by rememberSaveable { mutableStateOf(initial.biographyZh.orEmpty()) }
    var avatarSource by rememberSaveable { mutableStateOf(initial.avatarSourceUrl.orEmpty()) }
    var publicName by rememberSaveable { mutableStateOf(initial.publicName.orEmpty()) }
    var summary by rememberSaveable { mutableStateOf(initial.summary.orEmpty()) }
    var remark by rememberSaveable { mutableStateOf(initial.remark.orEmpty()) }
    var note by rememberSaveable { mutableStateOf(initial.note.orEmpty()) }
    var accounts by rememberSaveable(stateSaver = accountSaver) { mutableStateOf(initial.accounts.orEmpty()) }
    var identity by rememberSaveable { mutableStateOf(initial.identityStatus ?: "empty") }
    var translationReviewed by rememberSaveable { mutableStateOf(false) }
    var editChinese by rememberSaveable { mutableStateOf(false) }
    val values = when (block.block) {
        "x" -> ProfileValues(handle = handle, displayName = displayName, profileUrl = profileUrl,
            biographyOriginal = original, biographyZh = translation, avatarSourceUrl = avatarSource)
        "basic" -> ProfileValues(publicName = publicName, summary = summary, remark = remark, note = note)
        else -> ProfileValues(accounts = accounts, identityStatus = identity)
    }
    val dirty = values != initial
    val stale = baseRevision != block.revision
    val needsTranslationReview = block.block == "x" && original != originalBaseline && translation.isNotBlank()
    val valid = when (block.block) {
        "x" -> handle.characters() <= 200 && displayName.characters() <= 200 && profileUrl.characters() <= 4096 &&
            original.characters() <= BIOGRAPHY_LIMIT && translation.characters() <= BIOGRAPHY_LIMIT && avatarSource.characters() <= 4096
        "basic" -> publicName.characters() <= 200 && summary.characters() <= BIOGRAPHY_LIMIT && remark.characters() <= 500 && note.characters() <= 10000
        "accounts" -> accounts.size <= 32 && accounts.all { it.platform.characters() <= 80 && it.identifier.characters() <= 200 && it.url.characters() <= 4096 && it.publishingUrl.characters() <= 4096 }
        else -> false
    }
    val issue = when {
        stale -> "资料已更新。请关闭草稿并刷新后继续编辑。"
        !valid -> "有字段超过允许长度，请缩短后保存。"
        else -> error
    }
    CreatorProfileEditorLayout(
        title = profileBlockName(block.block), authorName = authorName, busy = busy, dirty = dirty,
        canSave = !stale && valid && (dirty || candidate != null) && (!needsTranslationReview || translationReviewed),
        error = issue, onClose = onClose,
        onSave = { model.save(document, block.copy(revision = baseRevision), values, candidate?.evidence ?: block.evidence,
            candidate?.id, translationReviewed, onClose) },
    ) {
        CreatorSection {
            if (candidate != null) Text("已载入候选草稿。核对后点保存资料。", style = MaterialTheme.typography.bodySmall)
            when (block.block) {
                "x" -> {
                    ProfileField("账号", handle, !busy) { handle = it }
                    ProfileField("显示名称", displayName, !busy) { displayName = it }
                    ProfileField("X 账号主页", profileUrl, !busy) { profileUrl = it }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        QimengSegPill("原文", !editChinese, { editChinese = false }, enabled = !busy)
                        QimengSegPill("中文", editChinese, { editChinese = true }, enabled = !busy)
                    }
                    ProfileField(
                        if (editChinese) "中文译文" else "X 简介原文",
                        if (editChinese) translation else original,
                        !busy, multiline = true, maxChars = BIOGRAPHY_LIMIT,
                        modifier = Modifier.testTag("author-biography-edit"),
                    ) { value ->
                        if (editChinese) translation = value
                        else if (value != original) { original = value; translation = "" }
                        translationReviewed = false
                    }
                    if (needsTranslationReview) Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(translationReviewed, { translationReviewed = it }, enabled = !busy)
                        Text("我已核对当前原文与中文译文", style = MaterialTheme.typography.bodySmall)
                    }
                    Text("修改原文会清除旧译文。重新填写译文后，请核对它与当前原文的对应关系。", style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant)
                    ProfileField("头像来源线索", avatarSource, !busy) { avatarSource = it }
                    Text("保存来源线索不会获取图片。请使用资料页的头像上传或网址获取入口。", style = MaterialTheme.typography.bodySmall)
                }
                "basic" -> {
                    ProfileField("公开昵称", publicName, !busy) { publicName = it }
                    Text("公开昵称与作者主名称分别保存。", style = MaterialTheme.typography.bodySmall)
                    ProfileField("整理简介", summary, !busy, multiline = true, maxChars = BIOGRAPHY_LIMIT) { summary = it }
                    ProfileField("特殊情况备注", remark, !busy, multiline = true, maxChars = 500) { remark = it }
                    ProfileField("私人备注", note, !busy, multiline = true, maxChars = 10_000) { note = it }
                }
                "accounts" -> {
                    Text("身份核实", style = MaterialTheme.typography.titleSmall)
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        listOf("empty" to "待完善", "candidate" to "待核实", "verified" to "已确认").forEach { (value, label) ->
                            FilterChip(selected = identity == value, onClick = { identity = value }, enabled = !busy, label = { Text(label) })
                        }
                    }
                    accounts.forEachIndexed { index, account ->
                        HorizontalDivider()
                        Text("账号 ${index + 1}", style = MaterialTheme.typography.titleSmall)
                        fun update(value: ProfileAccount) { accounts = accounts.toMutableList().also { it[index] = value } }
                        ProfileField("平台", account.platform, !busy) { update(account.copy(platform = it)) }
                        ProfileField("账号标识", account.identifier, !busy) { update(account.copy(identifier = it)) }
                        ProfileField("主页", account.url, !busy) { update(account.copy(url = it)) }
                        ProfileField("发布入口", account.publishingUrl, !busy) { update(account.copy(publishingUrl = it)) }
                        TextButton(onClick = { accounts = accounts.filterIndexed { i, _ -> i != index } }, enabled = !busy) { Text("移除此账号") }
                    }
                    TextButton(onClick = { accounts = accounts + ProfileAccount("", "", "", "") }, enabled = !busy && accounts.size < 32) { Text("添加账号") }
                }
            }
            if (candidate?.evidence?.isNotEmpty() == true) {
                Text("候选依据将随资料保留，原核实状态不自动改变。", style = MaterialTheme.typography.bodySmall)
                candidate.evidence.forEach { Text("${it.title}\n${it.url}", style = MaterialTheme.typography.bodySmall) }
            }
        }
    }
}

@Composable
internal fun CreatorProfileEditorLayout(
    title: String,
    authorName: String,
    busy: Boolean,
    dirty: Boolean,
    canSave: Boolean,
    error: String?,
    onClose: () -> Unit,
    onSave: () -> Unit,
    saveLabel: String = "保存资料",
    content: @Composable ColumnScope.() -> Unit,
) {
    var confirmDiscard by rememberSaveable { mutableStateOf(false) }
    val close = { if (!busy) { if (dirty) confirmDiscard = true else onClose() } }
    Dialog(onDismissRequest = close, properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        val view = LocalView.current
        val light = MaterialTheme.colorScheme.background.luminance() > 0.5f
        SideEffect {
            (view.parent as? DialogWindowProvider)?.window?.let { window ->
                WindowCompat.getInsetsController(window, view).apply {
                    isAppearanceLightStatusBars = light
                    isAppearanceLightNavigationBars = light
                }
            }
        }
        Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
            Column(Modifier.fillMaxSize().safeDrawingPadding().imePadding()) {
                QimengTopBar(title, Modifier.testTag("author-profile-editor-top-bar"), onBack = close)
                Column(
                    Modifier.weight(1f).verticalScroll(rememberScrollState()).testTag("author-profile-editor-body")
                        .padding(horizontal = QimengDimens.ScreenPaddingHorizontal, vertical = 16.dp),
                    verticalArrangement = Arrangement.spacedBy(18.dp),
                ) {
                    if (authorName.isNotBlank()) Text(authorName, style = MaterialTheme.typography.titleLarge)
                    Text("正在编辑 · $title", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    content()
                }
                Column(Modifier.fillMaxWidth().testTag("author-profile-save-bar")) {
                    HorizontalDivider()
                    val status = error ?: when { busy -> "正在保存…"; dirty -> "有未保存的修改"; else -> "已与当前资料一致" }
                    Text(status, Modifier.padding(horizontal = QimengDimens.ScreenPaddingHorizontal, vertical = 8.dp),
                        style = MaterialTheme.typography.bodySmall,
                        color = if (error == null) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.error)
                    Row(
                        Modifier.fillMaxWidth().padding(horizontal = QimengDimens.ScreenPaddingHorizontal, vertical = 12.dp),
                        horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically,
                    ) {
                        TextButton(onClick = close, enabled = !busy) { Text("取消") }
                        Button(onClick = onSave, enabled = !busy && canSave, modifier = Modifier.weight(1f)) {
                            Text(if (busy) "保存中…" else saveLabel)
                        }
                    }
                }
            }
        }
    }
    if (confirmDiscard) AlertDialog(onDismissRequest = { confirmDiscard = false }, title = { Text("放弃未保存的修改？") },
        confirmButton = { TextButton(onClick = { confirmDiscard = false; onClose() }) { Text("放弃修改") } },
        dismissButton = { TextButton(onClick = { confirmDiscard = false }) { Text("继续编辑") } })
}

@Composable
private fun ProfileField(label: String, value: String, enabled: Boolean, multiline: Boolean = false,
                         maxChars: Int? = null, modifier: Modifier = Modifier, onChange: (String) -> Unit) {
    OutlinedTextField(value, onChange, enabled = enabled, modifier = modifier.fillMaxWidth(), label = { Text(label) },
        singleLine = !multiline, minLines = if (multiline) 4 else 1, maxLines = if (multiline) 8 else 1,
        supportingText = maxChars?.let { limit -> { Text("${value.characters()} / $limit") } })
}

private fun String.characters() = codePointCount(0, length)

/** A missing candidate field preserves the current value; an empty field explicitly clears it. */
internal fun profileDraftValues(block: ProfileBlock, candidate: ProfileCandidate?): ProfileValues {
    val current = block.values
    val proposed = candidate?.values ?: ProfileValues()
    return when (block.block) {
        "x" -> ProfileValues(handle = proposed.handle ?: current.handle.orEmpty(), displayName = proposed.displayName ?: current.displayName.orEmpty(),
            profileUrl = proposed.profileUrl ?: current.profileUrl.orEmpty(), biographyOriginal = proposed.biographyOriginal ?: current.biographyOriginal.orEmpty(),
            biographyZh = proposed.biographyZh ?: if (proposed.biographyOriginal != null && proposed.biographyOriginal != current.biographyOriginal.orEmpty()) "" else current.biographyZh.orEmpty(),
            avatarSourceUrl = proposed.avatarSourceUrl ?: current.avatarSourceUrl.orEmpty())
        "basic" -> ProfileValues(publicName = proposed.publicName ?: current.publicName.orEmpty(), summary = proposed.summary ?: current.summary.orEmpty(),
            remark = proposed.remark ?: current.remark.orEmpty(), note = proposed.note ?: current.note.orEmpty())
        else -> ProfileValues(accounts = proposed.accounts ?: current.accounts.orEmpty(), identityStatus = proposed.identityStatus ?: current.identityStatus ?: "empty")
    }
}
