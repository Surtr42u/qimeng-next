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
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
        item { Row {
            TextButton(onClick = { model.load(authorId, archiveId) }, enabled = !busy) { Text("刷新资料") }
            if (archiveId != null) Text("历史档案 · 只读")
        } }
        if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
        state.error?.let { item { ErrorNotice(it) { model.load(authorId, archiveId) } } }
        error?.let { item { ErrorNotice(it) } }
        if (document != null) {
            items(document.blocks, key = { it.block }) { block -> ContentCard {
                Text(profileBlockName(block.block), style = MaterialTheme.typography.titleLarge)
                Text(sourceName(block.source), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                block.avatar?.let { ProfileAvatarImage(it, 96.dp) }
                ProfileValueText(block.values)
                if (block.evidence.isNotEmpty()) {
                    Text("依据", style = MaterialTheme.typography.titleSmall)
                    block.evidence.forEach { EvidenceText(it) }
                }
                if (document.editable && archiveId == null) {
                    TextButton(onClick = { candidateId = null; editing = block.block }, enabled = !busy && !state.loading) { Text("编辑${profileBlockName(block.block)}") }
                    if (block.block == "x") {
                        Row {
                            TextButton(onClick = { model.prepareAvatar(document, block.revision); picker.launch(arrayOf("image/jpeg", "image/png", "image/gif", "image/webp")) }, enabled = !busy) { Text("上传头像") }
                            TextButton(onClick = { remoteAvatar = true }, enabled = !busy) { Text("从网址获取头像") }
                        }
                        if (block.avatar != null) TextButton(onClick = { clearAvatar = true }, enabled = !busy) { Text("清除头像") }
                    }
                }
            } }
            item { TextButton(onClick = { showCandidates = !showCandidates }) { Text(if (showCandidates) "收起候选资料" else "查看候选资料") } }
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
                        Row {
                            TextButton(onClick = { candidateId = candidate.id; editing = candidate.block }, enabled = !busy && current?.revision == candidate.baseRevision) { Text("载入候选草稿") }
                            TextButton(onClick = { model.dismiss(document, candidate) }, enabled = !busy) { Text("不采用") }
                        }
                    }
                } }
                if (candidates.cursor != null) item { TextButton(onClick = { model.loadCandidates(true) }, enabled = !candidates.loading) { Text("更多候选") } }
            }
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
    }
    if (document != null) {
        val block = document.blocks.find { it.block == editing }
        val candidate = candidates.items.find { it.id == candidateId }
        if (block != null && (candidateId == null || candidate != null)) ProfileEditor(document, block, candidate, model) { editing = null; candidateId = null }
        else if (editing != null && candidateId != null) AlertDialog(onDismissRequest = { editing = null; candidateId = null },
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
private fun ProfileValueText(values: ProfileValues) {
    values.identityStatus?.let { Text(mapOf("empty" to "资料待完善", "candidate" to "资料待核实", "verified" to "身份已确认")[it] ?: it) }
    listOf("账号" to values.handle, "显示名称" to values.displayName, "X 主页" to values.profileUrl,
        "简介原文" to values.biographyOriginal, "中文译文" to values.biographyZh, "公开昵称" to values.publicName,
        "概要" to values.summary, "特殊情况备注" to values.remark, "私人备注" to values.note).forEach { (name, value) ->
        if (!value.isNullOrEmpty()) { Text(name, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant); Text(value) }
    }
    values.accounts.orEmpty().forEach { account ->
        Text("${account.platform} · ${account.identifier}", style = MaterialTheme.typography.titleSmall)
        if (account.url.isNotEmpty()) Text(account.url)
        if (account.publishingUrl.isNotEmpty()) Text("发布入口：${account.publishingUrl}")
    }
}

@Composable
private fun EvidenceText(evidence: ProfileEvidence) {
    Text("${evidence.title} · ${mapOf("unverified" to "未核实", "candidate" to "候选依据", "confirmed" to "已确认依据")[evidence.status] ?: "状态待核对"}", style = MaterialTheme.typography.bodySmall)
    if (evidence.url.isNotEmpty()) Text(evidence.url, style = MaterialTheme.typography.bodySmall)
    if (evidence.note.isNotEmpty()) Text(evidence.note, style = MaterialTheme.typography.bodySmall)
}

internal fun profileBlockName(value: String) = mapOf("x" to "X 资料", "basic" to "基本资料", "accounts" to "其他账号")[value] ?: value
private fun sourceName(value: String) = mapOf("empty" to "尚未完善", "human" to "人工保存", "merged" to "合并保留", "imported" to "导入资料")[value] ?: value
private fun candidateState(value: String) = mapOf("pending" to "待确认候选", "applied" to "已采用", "dismissed" to "未采用")[value] ?: value
