package media.qimeng.next.screen

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import coil3.compose.AsyncImage
import coil3.request.ImageRequest
import media.qimeng.next.AuthorProfileViewModel
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.*
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.QimengTopBar
import media.qimeng.next.ui.theme.QimengDimens
import java.text.DateFormat
import java.time.OffsetDateTime
import java.util.Date

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun AuthorProfileScreen(authorId: String, library: LibraryViewModel, archiveId: String? = null, model: AuthorProfileViewModel = viewModel()) {
    val state by model.profile.collectAsStateWithLifecycle()
    val candidates by model.candidates.collectAsStateWithLifecycle()
    val archives by model.archives.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val error by model.error.collectAsStateWithLifecycle()
    var editing by rememberSaveable(authorId, archiveId) { mutableStateOf<String?>(null) }
    var candidateId by rememberSaveable(authorId, archiveId) { mutableStateOf<String?>(null) }
    var showCandidates by rememberSaveable(authorId, archiveId) { mutableStateOf(false) }
    var showArchives by rememberSaveable(authorId, archiveId) { mutableStateOf(false) }
    var remoteAvatar by rememberSaveable(authorId, archiveId) { mutableStateOf(false) }
    var clearAvatar by rememberSaveable(authorId, archiveId) { mutableStateOf(false) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument(), model::uploadAvatar)
    LaunchedEffect(authorId, archiveId) { model.load(authorId, archiveId) }
    LaunchedEffect(showCandidates, authorId, archiveId) { if (showCandidates) model.loadCandidates() }
    LaunchedEffect(showArchives, authorId) { if (showArchives) model.loadArchives() }
    val document = state.data?.takeIf { it.authorId == authorId }
    val author by library.author.collectAsStateWithLifecycle()
    val entry = author?.takeIf { it.id == authorId && archiveId == null }
    Column(Modifier.fillMaxSize()) {
      QimengTopBar("资料与维护", Modifier.testTag("author-profile-top-bar"), onBack = library::back)
      LazyColumn(
          Modifier.weight(1f).navigationBarsPadding().testTag("author-profile-overview"),
          contentPadding = PaddingValues(horizontal = QimengDimens.ScreenPaddingHorizontal, vertical = 12.dp),
          verticalArrangement = Arrangement.spacedBy(16.dp),
      ) {
        item(key = "author-identity") {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (entry != null) {
                    Text(entry.name, style = MaterialTheme.typography.titleLarge)
                }
                if (archiveId != null) CreatorDetailLabel("历史档案 · 只读")
                document?.blocks?.find { it.block == "accounts" }?.let { CreatorDetailLabel(identityLabel(it.values.identityStatus)) }
            }
        }
        if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
        state.error?.let { item { ErrorNotice(it) { model.load(authorId, archiveId) } } }
        if (editing == null) error?.let { item { ErrorNotice(it) } }
        if (document != null) {
            items(document.blocks, key = { it.block }) { block ->
              val editable = document.editable && archiveId == null
              val startEdit: (() -> Unit)? = if (editable) ({ candidateId = null; editing = block.block }) else null
              CreatorProfileGroup(profileBlockName(block.block), !busy && !state.loading, startEdit) {
                when (block.block) {
                    "x" -> CreatorXDetails(block.values)
                    "basic" -> CreatorBasicDetails(entry, block.values)
                    "accounts" -> CreatorAccountDetails(block.values)
                }
                block.avatar?.let { ProfileAvatarImage(it, 96.dp) }
                if (editable && block.block == "x") FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    TextButton(onClick = { model.prepareAvatar(document, block.revision); picker.launch(arrayOf("image/jpeg", "image/png", "image/gif", "image/webp")) }, enabled = !busy) { Text("上传头像") }
                    TextButton(onClick = { remoteAvatar = true }, enabled = !busy) { Text("从网址获取头像") }
                    if (block.avatar != null) TextButton(onClick = { clearAvatar = true }, enabled = !busy) { Text("清除头像") }
                }
                CreatorEvidence(block)
              }
            }
            if (archiveId == null) item(key = "author-subscription") { AuthorUpdatesSection(authorId, document, settings = true, details = {}, authorName = entry?.name.orEmpty()) }
            item(key = "author-candidates") {
                CreatorSection {
                    Text("候选资料", style = MaterialTheme.typography.titleSmall)
                    TextButton(onClick = { showCandidates = !showCandidates }) { Text(if (showCandidates) "收起候选资料" else "查看候选资料") }
                }
            }
            if (showCandidates) {
                candidates.error?.let { item { ErrorNotice(it) { model.loadCandidates() } } }
                if (candidates.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
                if (candidates.loaded && candidates.items.isEmpty()) item { Text("暂无候选资料") }
                items(candidates.items, key = { "candidate:${it.id}" }) { candidate -> ContentCard {
                    Text("${profileBlockName(candidate.block)} · ${candidateState(candidate.state)}", style = MaterialTheme.typography.titleMedium)
                    ProfileValueText(candidate.values)
                    candidate.evidence.forEach { EvidenceText(it) }
                    val current = document.blocks.find { it.block == candidate.block }
                    if (candidate.state == "pending" && document.editable && archiveId == null) {
                        if (current?.revision != candidate.baseRevision) Text("当前资料已变化。请对照候选，手工编辑当前资料；不会自动覆盖。")
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            TextButton(onClick = { candidateId = candidate.id; editing = candidate.block }, enabled = !busy && current?.revision == candidate.baseRevision) { Text("载入候选草稿") }
                            TextButton(onClick = { model.dismiss(document, candidate) }, enabled = !busy) { Text("不采用") }
                        }
                    }
                } }
                if (candidates.cursor != null) item { TextButton(onClick = { model.loadCandidates(true) }, enabled = !candidates.loading) { Text("更多候选") } }
            }
            if (document.editable) entry?.takeIf { it.kind != "cos" }?.let { item(key = "author-management") { AuthorManagementTools(library, it) } }
            if (archiveId == null) item { TextButton(onClick = { showArchives = !showArchives }) { Text(if (showArchives) "收起保留档案" else "查看合并 / 删除保留档案") } }
            if (showArchives) {
                archives.error?.let { item { ErrorNotice(it) { model.loadArchives() } } }
                items(archives.items, key = { "archive:${it.id}" }) { archive -> ContentCard {
                    Text(archive.authorName, style = MaterialTheme.typography.titleMedium)
                    Text("${archive.createdAt} · ${archive.blockCount} 个资料块 / ${archive.candidateCount} 个候选")
                    if (archive.conflicts.isNotEmpty()) Text("保留差异：${archive.conflicts.joinToString("、")}")
                    TextButton(onClick = { library.navigate("profile-archive:$authorId:${archive.id}") }) { Text("查看只读档案") }
                } }
                if (archives.cursor != null) item { TextButton(onClick = { model.loadArchives(true) }, enabled = !archives.loading) { Text("更多档案") } }
                if (archives.loaded && archives.items.isEmpty()) item { Text("暂无保留档案") }
            }
        }
        item { TextButton(onClick = { model.load(authorId, archiveId) }, enabled = !busy && !state.loading) { Text("刷新资料") } }
    }
    }
    if (document != null) {
        val block = document.blocks.find { it.block == editing }
        val candidate = candidates.items.find { it.id == candidateId }
        if (block != null && document.editable && (candidateId == null || candidate != null)) ProfileEditor(entry?.name.orEmpty(), document, block, candidate, model) { editing = null; candidateId = null }
        else if (editing != null && candidateId != null && (candidates.loaded || candidates.error != null)) AlertDialog(onDismissRequest = { editing = null; candidateId = null },
            title = { Text("请重新选择候选") }, text = { Text("当前列表尚未载入这条候选，不能把它当作当前资料。") },
            confirmButton = { TextButton(onClick = { editing = null; candidateId = null; showCandidates = true; model.loadCandidates() }) { Text("载入候选列表") } })
        val x = document.blocks.find { it.block == "x" }
        if (remoteAvatar && x != null) TextEditDialog("公开 HTTPS 头像地址", x.values.avatarSourceUrl.orEmpty(), busy, { remoteAvatar = false }) {
            model.fetchAvatar(document, x, it.trim()) { remoteAvatar = false }
        }
        if (clearAvatar && x != null) AlertDialog(onDismissRequest = { clearAvatar = false }, title = { Text("清除已保存的头像？") },
            text = { Text("其他 X 资料、作者身份和媒体关联保持。") }, confirmButton = { TextButton(onClick = { model.clearAvatar(document, x.revision); clearAvatar = false }, enabled = !busy) { Text("清除头像") } },
            dismissButton = { TextButton(onClick = { clearAvatar = false }) { Text("取消") } })
    }
}

@Composable
fun AuthorAvatar(authorId: String, name: String, size: Dp = 44.dp, model: AuthorProfileViewModel = viewModel()) {
    val state by model.summary(authorId).collectAsStateWithLifecycle()
    LaunchedEffect(authorId) { model.loadSummary(authorId) }
    val avatar = state.data?.avatar
    if (avatar != null) ProfileAvatarImage(avatar, size) else Box(Modifier.size(size).clip(CircleShape).background(MaterialTheme.colorScheme.surfaceVariant), contentAlignment = Alignment.Center) {
        Text(name.take(1), style = MaterialTheme.typography.titleLarge)
    }
}

@Composable
fun AuthorProfileSummary(authorId: String, model: AuthorProfileViewModel = viewModel()) {
    val state by model.summary(authorId).collectAsStateWithLifecycle()
    LaunchedEffect(authorId) { model.loadSummary(authorId) }
    state.error?.let { ErrorNotice(it) { model.loadSummary(authorId, force = true) } }
    state.data?.let { summary ->
        if (summary.biographyOriginal.isNotEmpty()) Text(summary.biographyOriginal, maxLines = 3, overflow = TextOverflow.Ellipsis)
        if (summary.biographyZh.isNotEmpty()) Text(summary.biographyZh, maxLines = 3, overflow = TextOverflow.Ellipsis, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun ProfileAvatarImage(avatar: ProfileAvatar, size: Dp) {
    val context = LocalContext.current
    AsyncImage(model = ImageRequest.Builder(context).data(avatar.url).memoryCacheKey(avatar.cacheKey).diskCacheKey(avatar.cacheKey).build(),
        imageLoader = LocalMediaImages.current, contentDescription = "服务器保存的作者头像", contentScale = ContentScale.Crop, modifier = Modifier.size(size).clip(CircleShape))
}

@Composable
private fun CreatorProfileGroup(title: String, enabled: Boolean, onEdit: (() -> Unit)?, content: @Composable ColumnScope.() -> Unit) {
    CreatorSection {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(title, Modifier.weight(1f), style = MaterialTheme.typography.titleSmall)
            if (onEdit != null) TextButton(onClick = onEdit, enabled = enabled) { Text("编辑$title") }
        }
        content()
    }
}

@Composable
private fun CreatorXDetails(values: ProfileValues) {
    var showTranslation by rememberSaveable { mutableStateOf(false) }
    Text(values.biographyOriginal?.takeIf(String::isNotBlank) ?: "尚未收录 X 简介。", style = MaterialTheme.typography.bodyMedium)
    values.biographyZh?.takeIf(String::isNotBlank)?.let { translated ->
        TextButton(onClick = { showTranslation = !showTranslation }) { Text(if (showTranslation) "收起中文译文" else "查看中文译文") }
        if (showTranslation) Text(translated, style = MaterialTheme.typography.bodyMedium)
    }
    val names = listOfNotNull(values.displayName, values.handle).filter(String::isNotBlank)
    if (names.isNotEmpty()) CreatorDetailLabel(names.joinToString(" · "))
    PublicAuthorLink("前往 X ↗", values.profileUrl)
}

@Composable
private fun CreatorBasicDetails(author: Author?, values: ProfileValues) {
    var showSummary by rememberSaveable { mutableStateOf(false) }
    if (author != null) {
        Text(author.txtOrder?.let { "作者清单第 $it 位" } ?: "未记录清单顺序", style = MaterialTheme.typography.bodyMedium)
        val receivedAt = author.createdAt?.let { Date.from(OffsetDateTime.parse(it).toInstant()) }
        CreatorDetailLabel(receivedAt?.let { "入库记录 ${DateFormat.getDateTimeInstance().format(it)}" } ?: "入库时间未记录")
        CreatorDetailLabel("历史批量导入仅能确认记录入库时间，原始加入日期未记录。")
    }
    CreatorDetailLabel("公开昵称")
    Text(values.publicName?.takeIf(String::isNotBlank) ?: "尚未填写", style = MaterialTheme.typography.bodyMedium)
    CreatorDetailLabel("整理简介")
    val summary = values.summary?.takeIf(String::isNotBlank)
    Text(summary?.substringBefore("\n\n") ?: "尚未填写", style = MaterialTheme.typography.bodyMedium,
        maxLines = 3, overflow = TextOverflow.Ellipsis)
    if (summary != null) {
        TextButton(onClick = { showSummary = !showSummary }) { Text(if (showSummary) "收起完整说明" else "查看完整说明") }
        if (showSummary) Text(summary, style = MaterialTheme.typography.bodyMedium)
    }
    listOf("特殊情况备注" to values.remark, "私人备注" to values.note).forEach { (label, text) ->
        if (!text.isNullOrBlank()) {
            CreatorDetailLabel(label)
            Text(text, style = MaterialTheme.typography.bodyMedium)
        }
    }
}

@Composable
private fun CreatorAccountDetails(values: ProfileValues) {
    val accounts = values.accounts.orEmpty()
    if (accounts.isEmpty()) CreatorDetailLabel("尚未填写账号来源")
    accounts.forEach { account ->
        Text(listOf(account.platform, account.identifier).filter(String::isNotBlank).joinToString(" · "), style = MaterialTheme.typography.titleSmall)
        PublicAuthorLink("主页 ↗", account.url)
        PublicAuthorLink("发布入口 ↗", account.publishingUrl)
    }
}

@Composable
private fun CreatorEvidence(block: ProfileBlock) {
    if (block.source == "empty" && block.evidence.isEmpty()) return
    var showEvidence by rememberSaveable { mutableStateOf(false) }
    TextButton(onClick = { showEvidence = !showEvidence }) { Text(if (showEvidence) "收起核验记录" else "查看核验记录") }
    if (showEvidence) {
        val provenance = listOfNotNull(sourceName(block.source), block.updatedAt).joinToString(" · ")
        CreatorDetailLabel(provenance)
        block.evidence.forEach { EvidenceText(it) }
    }
}

@Composable
private fun CreatorDetailLabel(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

@Composable
private fun ProfileValueText(values: ProfileValues) {
    values.identityStatus?.let { Text(identityLabel(it)) }
    listOf("账号" to values.handle, "显示名称" to values.displayName, "X 主页" to values.profileUrl,
        "简介原文" to values.biographyOriginal, "中文译文" to values.biographyZh, "公开昵称" to values.publicName,
        "整理简介" to values.summary, "特殊情况备注" to values.remark, "私人备注" to values.note).forEach { (name, value) ->
        if (!value.isNullOrEmpty()) {
            CreatorDetailLabel(name)
            if (name == "X 主页") PublicAuthorLink(value, value)
            else Text(value)
        }
    }
    if (values.accounts != null) CreatorAccountDetails(values)
}

@Composable
private fun EvidenceText(evidence: ProfileEvidence) {
    Text("${evidence.title} · ${mapOf("unverified" to "未核实", "candidate" to "候选依据", "confirmed" to "已确认依据")[evidence.status] ?: "状态待核对"}", style = MaterialTheme.typography.bodySmall)
    if (evidence.url.isNotEmpty()) Text(evidence.url, style = MaterialTheme.typography.bodySmall)
    if (evidence.note.isNotEmpty()) Text(evidence.note, style = MaterialTheme.typography.bodySmall)
    evidence.checkedAt?.let { CreatorDetailLabel(it) }
}

internal fun profileBlockName(value: String) = mapOf("x" to "X 资料", "basic" to "基础资料", "accounts" to "账号来源")[value] ?: value
private fun identityLabel(value: String?) = when (value) { "verified" -> "身份已确认"; "candidate" -> "资料待核实"; else -> "资料待完善" }
private fun sourceName(value: String) = mapOf("empty" to "尚未完善", "human" to "人工保存", "merged" to "合并保留", "imported" to "导入资料", "native_delivery" to "原生交付")[value] ?: value
private fun candidateState(value: String) = mapOf("pending" to "待确认候选", "applied" to "已采用", "dismissed" to "未采用")[value] ?: value
