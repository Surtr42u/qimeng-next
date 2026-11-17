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
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.AuthorProfileViewModel
import media.qimeng.next.model.*
import media.qimeng.next.ui.ErrorNotice

private val accountSaver = listSaver<List<ProfileAccount>, String>(
    save = { accounts -> accounts.flatMap { listOf(it.platform, it.identifier, it.url, it.publishingUrl) } },
    restore = { values -> values.chunked(4).map { ProfileAccount(it[0], it[1], it[2], it[3]) } })

@Composable
internal fun ProfileEditor(document: ProfileDocument, block: ProfileBlock, candidate: ProfileCandidate?, model: AuthorProfileViewModel, onClose: () -> Unit) {
    key(document.target.baseUrl, document.target.instanceId, document.authorId, block.block, candidate?.id) {
        ProfileEditorContent(document, block, candidate, model, onClose)
    }
}

@Composable
private fun ProfileEditorContent(document: ProfileDocument, block: ProfileBlock, candidate: ProfileCandidate?, model: AuthorProfileViewModel, onClose: () -> Unit) {
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
    var confirmDiscard by rememberSaveable { mutableStateOf(false) }
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
        "x" -> handle.length <= 200 && displayName.length <= 200 && profileUrl.length <= 4096 && original.length <= 20000 && translation.length <= 20000 && avatarSource.length <= 4096
        "basic" -> publicName.length <= 200 && summary.length <= 20000 && remark.length <= 500 && note.length <= 10000
        "accounts" -> accounts.size <= 32 && accounts.all { it.platform.length <= 80 && it.identifier.length <= 200 && it.url.length <= 4096 && it.publishingUrl.length <= 4096 }
        else -> false
    }
    val close = { if (!busy) { if (dirty) confirmDiscard = true else onClose() } }
    AlertDialog(onDismissRequest = close, title = { Text("编辑${profileBlockName(block.block)}") }, text = {
        Column(Modifier.heightIn(max = 500.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (candidate != null) Text("已载入候选草稿。请核对后明确保存；原资料尚未改变。")
            when (block.block) {
                "x" -> {
                    ProfileField("账号", handle, !busy) { handle = it }
                    ProfileField("显示名称", displayName, !busy) { displayName = it }
                    ProfileField("X 账号主页", profileUrl, !busy) { profileUrl = it }
                    ProfileField("简介原文", original, !busy, true) { original = it; translationReviewed = false }
                    ProfileField("中文译文", translation, !busy, true) { translation = it; translationReviewed = false }
                    if (needsTranslationReview) Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(translationReviewed, { translationReviewed = it }, enabled = !busy)
                        Text("我已核对当前原文与中文译文", style = MaterialTheme.typography.bodySmall)
                    }
                    if (original != originalBaseline) Text("原文已改变。留空译文将清除旧译文；同时保存译文需要确认对应关系。", style = MaterialTheme.typography.bodySmall)
                    ProfileField("头像来源线索", avatarSource, !busy) { avatarSource = it }
                    Text("保存来源线索不会获取图片。请使用资料页的头像上传或网址获取入口。", style = MaterialTheme.typography.bodySmall)
                }
                "basic" -> {
                    ProfileField("公开昵称", publicName, !busy) { publicName = it }
                    Text("公开昵称与作者主名称分别保存。", style = MaterialTheme.typography.bodySmall)
                    ProfileField("概要", summary, !busy, true) { summary = it }
                    ProfileField("特殊情况备注（最多 500 字）", remark, !busy, true) { remark = it }
                    ProfileField("私人备注", note, !busy, true) { note = it }
                }
                "accounts" -> {
                    Text("身份核实", style = MaterialTheme.typography.titleSmall)
                    listOf("empty" to "待完善", "candidate" to "待核实", "verified" to "身份已确认").forEach { (value, label) ->
                        Row(verticalAlignment = Alignment.CenterVertically) { RadioButton(identity == value, { identity = value }, enabled = !busy); Text(label) }
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
            if (stale) ErrorNotice("资料已在其他操作中改变，请关闭草稿、刷新后重新编辑。当前草稿不会覆盖新资料。")
            if (!valid) ErrorNotice("有字段超过允许长度，请缩短后保存。")
            error?.let { ErrorNotice(it) }
        }
    }, confirmButton = { TextButton(onClick = {
        model.save(document, block.copy(revision = baseRevision), values, candidate?.evidence ?: block.evidence,
            candidate?.id, translationReviewed, onClose)
    }, enabled = !busy && !stale && valid && (dirty || candidate != null) && (!needsTranslationReview || translationReviewed)) { Text(if (busy) "保存中…" else "保存资料") } },
        dismissButton = { TextButton(onClick = close, enabled = !busy) { Text("取消") } })
    if (confirmDiscard) AlertDialog(onDismissRequest = { confirmDiscard = false }, title = { Text("放弃未保存的修改？") },
        confirmButton = { TextButton(onClick = onClose) { Text("放弃修改") } },
        dismissButton = { TextButton(onClick = { confirmDiscard = false }) { Text("继续编辑") } })
}

@Composable
private fun ProfileField(label: String, value: String, enabled: Boolean, multiline: Boolean = false, onChange: (String) -> Unit) {
    OutlinedTextField(value, onChange, enabled = enabled, modifier = Modifier.fillMaxWidth(), label = { Text(label) },
        singleLine = !multiline, minLines = if (multiline) 3 else 1, maxLines = if (multiline) 8 else 1)
}

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
