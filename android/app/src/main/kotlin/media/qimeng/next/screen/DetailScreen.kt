package media.qimeng.next.screen

import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil3.compose.AsyncImage
import coil3.request.CachePolicy
import coil3.request.ImageRequest
import coil3.size.Size
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Asset
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice
import java.text.Collator
import java.util.Locale

@Composable
fun DetailScreen(model: LibraryViewModel, id: String) {
    val asset by model.asset.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val assetAuthors by model.assetAuthors.collectAsStateWithLifecycle()
    var showTags by rememberSaveable(id) { mutableStateOf(false) }
    var showNote by rememberSaveable(id) { mutableStateOf(false) }
    var showInfo by rememberSaveable(id) { mutableStateOf(false) }
    var showRename by rememberSaveable(id) { mutableStateOf(false) }
    var showTrash by rememberSaveable(id) { mutableStateOf(false) }
    var showAuthors by rememberSaveable(id) { mutableStateOf(false) }
    var showRelations by rememberSaveable(id) { mutableStateOf(false) }
    var showAcquisition by rememberSaveable(id) { mutableStateOf(false) }
    LaunchedEffect(id) { if (asset?.id != id) model.openAsset(id) }
    val item = asset?.takeIf { it.id == id }
    if (item == null) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { TextButton(onClick = { model.openAsset(id) }, enabled = !busy) { Text("正在读取详情 · 点击重试") } }
        return
    }
    TrackDwell(item, model)
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).navigationBarsPadding(), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (item.mediaType == "video") VideoStage(item, model) else ImageStage(item, model::adjacentAsset) { model.openAsset(item.id) }
        ContentCard(Modifier.padding(horizontal = 16.dp)) {
            Text(item.name, style = MaterialTheme.typography.titleLarge)
            item.collectionId?.let { collection -> TextButton(onClick = { model.navigate("collection:$collection") }) { Text("同组作品${item.page?.let { " · 第 $it 项" }.orEmpty()}") } }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(item.favorite, { model.changeReaction(!item.favorite, item.liked) }, label = { Text("收藏") }, enabled = !busy,
                    leadingIcon = { Icon(Icons.Outlined.Bookmark, null, Modifier.size(18.dp)) })
                FilterChip(item.liked, { model.changeReaction(item.favorite, true) }, label = { Text(if (item.liked) "今日已赞 · ${item.likeCount}" else "点赞 · ${item.likeCount}") }, enabled = !busy && !item.liked,
                    leadingIcon = { Icon(Icons.Outlined.FavoriteBorder, null, Modifier.size(18.dp)) })
                TextButton(onClick = { showInfo = true }) { Text("详细信息") }
            }
            assetAuthors.forEach { author -> TextButton(onClick = { model.openAuthor(author.id) }) { Text(author.name) } }
            if (assetAuthors.isEmpty()) item.authorId?.let { authorId -> TextButton(onClick = { model.openAuthor(authorId) }) { Text(item.authorName?.takeIf(String::isNotBlank) ?: "查看作者档案") } }
            TextButton(onClick = { showAuthors = true; model.loadAuthors() }) { Text("修改关联作者") }
            if (item.positionMs > 0 && item.positionVersion != item.version) Text("旧内容版本的播放位置为 ${item.positionMs / 1000} 秒。媒体已替换，本次从头播放。", color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text("标签", style = MaterialTheme.typography.titleSmall)
            Text(if (item.tags.isEmpty()) "暂无标签" else item.tags.joinToString(" · "), color = MaterialTheme.colorScheme.onSurfaceVariant)
            TextButton(onClick = { showTags = true; model.loadTags() }) { Text("管理标签") }
            Text("备注", style = MaterialTheme.typography.titleSmall)
            Text(item.note.ifBlank { "暂无备注" })
            TextButton(onClick = { showNote = true }) { Text("编辑备注") }
            TextButton(onClick = { showRelations = true }) { Text("原作 / 角色 · ${item.sourceIds.size} 个出处 / ${item.characterIds.size} 个角色") }
            if (item.acquisitionSources.isNotEmpty()) Text(item.acquisitionSources.joinToString("\n"), style = MaterialTheme.typography.bodySmall)
            TextButton(onClick = { showAcquisition = true }) { Text("编辑获取渠道 / 来源") }
            Row {
                TextButton(onClick = { showRename = true }) { Text("改名 / 移动") }
                TextButton(onClick = { showTrash = true }) { Text("移到回收站", color = MaterialTheme.colorScheme.error) }
            }
        }
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), horizontalArrangement = Arrangement.SpaceBetween) {
            OutlinedButton(onClick = { model.adjacentAsset(-1) }) { Text("上一项") }
            OutlinedButton(onClick = { model.adjacentAsset(1) }) { Text("下一项") }
        }
        Spacer(Modifier.height(24.dp))
    }
    if (showTags) TagsDialog(item, model) { showTags = false }
    if (showAuthors) AuthorsDialog(item, model) { showAuthors = false }
    if (showRelations) AssetRelationsDialog(item, model) { showRelations = false }
    if (showAcquisition) TextEditDialog("获取渠道 / 来源（每行一条）", item.acquisitionSources.joinToString("\n"), busy, { showAcquisition = false }) {
        model.saveAcquisitionSources(it.lines().filter(String::isNotBlank)) { showAcquisition = false }
    }
    if (showNote) TextEditDialog("编辑备注", item.note, busy, { showNote = false }) { value -> model.saveNote(value) { showNote = false } }
    if (showRename) TextEditDialog("库内路径与文件名", item.relativePath, busy, { showRename = false }) { value -> model.renameCurrentAsset(value) { showRename = false } }
    if (showTrash) AlertDialog(onDismissRequest = { showTrash = false }, title = { Text("移到回收站？") }, text = { Text(item.name) },
        confirmButton = { TextButton(onClick = { showTrash = false; model.trashCurrentAsset() }, enabled = !busy) { Text("移到回收站") } },
        dismissButton = { TextButton(onClick = { showTrash = false }) { Text("取消") } })
    if (showInfo) {
        val clipboard = LocalClipboardManager.current
        var copied by remember { mutableStateOf(false) }
        AlertDialog(onDismissRequest = { showInfo = false }, title = { Text("详细信息") },
            text = { Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(item.name)
                if (item.originalName != item.name) Text("来源原名：${item.originalName}")
                Text("${item.mediaType} · ${formatBytes(item.bytes)}")
                if (item.width != null && item.height != null) Text("${item.width} × ${item.height}")
                item.durationMs?.let { Text("时长 ${it / 1000} 秒") }
                Text("入库时间：${item.addedAt}")
                Text("内容版本 ${item.version}")
                TextButton(onClick = { clipboard.setText(AnnotatedString(item.name)); copied = true }) { Text(if (copied) "已复制文件名" else "复制完整文件名") }
            } }, confirmButton = { TextButton(onClick = { showInfo = false }) { Text("关闭") } })
    }
}

@Composable
private fun AuthorsDialog(asset: Asset, model: LibraryViewModel, onClose: () -> Unit) {
    val state by model.authors.collectAsStateWithLifecycle()
    val libraries by model.libraries.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    val kind = libraries.find { it.id == asset.libraryId }?.kind
    var selected by rememberSaveable(asset.id) { mutableStateOf(asset.authorIds) }
    AlertDialog(onDismissRequest = { if (!busy) onClose() }, title = { Text("关联作者 · ${selected.size}") },
        text = { Column(Modifier.heightIn(max = 460.dp)) {
            LazyColumn(Modifier.weight(1f, fill = false)) {
                items(state.items.filter { kind == null || it.kind == kind }, key = { it.id }) { author ->
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(author.id in selected, { checked -> selected = if (checked) selected + author.id else selected - author.id })
                        Text(author.name)
                    }
                }
                if (state.cursor != null) item { TextButton(onClick = { model.loadAuthors(true) }, enabled = !state.loading) { Text("加载更多作者") } }
            }
            state.error?.let { ErrorNotice(it) { model.loadAuthors() } }
            message?.let { ErrorNotice(it) }
        } }, confirmButton = { TextButton(onClick = { model.saveAuthors(selected, onClose) }, enabled = !busy && !state.loading) { Text("保存") } },
        dismissButton = { TextButton(onClick = onClose, enabled = !busy) { Text("取消") } })
}

@Composable
private fun ImageStage(asset: Asset, onNavigate: (Int) -> Unit, onReload: () -> Unit) {
    var scale by rememberSaveable(asset.id, asset.version) { mutableFloatStateOf(1f) }
    var offset by remember { mutableStateOf(Offset.Zero) }
    val context = LocalContext.current
    var error by remember(asset.id) { mutableStateOf(false) }
    var retry by remember(asset.id) { mutableIntStateOf(0) }
    Box(Modifier.fillMaxWidth().heightIn(min = 280.dp, max = 620.dp).aspectRatio(0.85f).background(Color.Black)
        .pointerInput(asset.id) { detectTapGestures(onDoubleTap = { scale = if (scale > 1f) 1f else 2.5f; offset = Offset.Zero }) }
        .pointerInput(asset.id) {
            awaitEachGesture {
                awaitFirstDown(requireUnconsumed = false)
                var horizontal = 0f
                var transformed = false
                do {
                    val event = awaitPointerEvent()
                    val pan = event.calculatePan()
                    if (event.changes.count { it.pressed } >= 2 || scale > 1f) {
                        transformed = true
                        scale = (scale * event.calculateZoom()).coerceIn(1f, 6f)
                        val maxX = size.width * (scale - 1f) / 2
                        val maxY = size.height * (scale - 1f) / 2
                        offset = Offset((offset.x + pan.x).coerceIn(-maxX, maxX), (offset.y + pan.y).coerceIn(-maxY, maxY))
                        event.changes.forEach { it.consume() }
                    } else {
                        horizontal += pan.x
                        if (kotlin.math.abs(horizontal) > viewConfiguration.touchSlop) event.changes.forEach { it.consume() }
                    }
                } while (event.changes.any { it.pressed })
                if (!transformed && kotlin.math.abs(horizontal) > size.width * 0.18f) onNavigate(if (horizontal < 0) 1 else -1)
            }
        }, contentAlignment = Alignment.Center) {
        AsyncImage(model = remember(asset.contentUrl, asset.version, retry) {
            ImageRequest.Builder(context).data(asset.contentUrl).size(Size.ORIGINAL)
                .diskCachePolicy(CachePolicy.DISABLED).memoryCacheKey("${asset.contentUrl}:${asset.version}").build()
        }, imageLoader = LocalMediaImages.current, contentDescription = asset.name, contentScale = ContentScale.Fit,
            onError = { error = true }, onSuccess = { error = false },
            modifier = Modifier.fillMaxSize().graphicsLayer { scaleX = scale; scaleY = scale; translationX = offset.x; translationY = offset.y })
        if (error) FilledTonalButton(onClick = { retry++; error = false; onReload() }) { Text("图片加载失败 · 重试") }
        Row(Modifier.align(Alignment.BottomEnd).padding(8.dp)) {
            IconButton(onClick = { scale = (scale - 0.5f).coerceAtLeast(1f); if (scale == 1f) offset = Offset.Zero }) { Icon(Icons.Outlined.ZoomOut, "缩小图片", tint = Color.White) }
            IconButton(onClick = { scale = (scale + 0.5f).coerceAtMost(6f) }) { Icon(Icons.Outlined.ZoomIn, "放大图片", tint = Color.White) }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun TagsDialog(asset: Asset, model: LibraryViewModel, onClose: () -> Unit) {
    val vocabulary by model.tagVocabulary.collectAsStateWithLifecycle()
    val cursor by model.tagCursor.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    var selected by rememberSaveable(asset.id) { mutableStateOf(asset.tags) }
    var baseRevision by rememberSaveable(asset.id) { mutableLongStateOf(asset.tagsRevision) }
    var extra by rememberSaveable { mutableStateOf("") }
    var added by rememberSaveable { mutableStateOf(emptyList<String>()) }
    val candidates = remember(vocabulary, asset.tags, added) { (vocabulary + asset.tags + added).distinct().sortedWith(::compareTags) }
    AlertDialog(onDismissRequest = { if (!busy) onClose() }, title = { Text("标签 · 已选 ${selected.size}") },
        text = { Column(Modifier.heightIn(max = 460.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            FlowRow(Modifier.height(76.dp).verticalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                selected.forEach { tag -> InputChip(selected = true, onClick = { }, label = { Text(tag) }, trailingIcon = {
                    IconButton(onClick = {
                        if (tag in asset.tags) {
                            val baseWasCurrent = baseRevision == asset.tagsRevision
                            model.removeTag(tag) { updated -> selected = selected - tag; if (baseWasCurrent) baseRevision = updated.tagsRevision }
                        } else selected = selected - tag
                    }, enabled = !busy, modifier = Modifier.size(24.dp)) { Icon(Icons.Outlined.Close, if (tag in asset.tags) "立即解除标签 $tag" else "移除草稿标签 $tag", Modifier.size(16.dp)) }
                }) }
            }
            FlowRow(Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                candidates.forEach { tag ->
                    FilterChip(tag in selected, { selected = if (tag in selected) selected - tag else selected + tag }, label = { Text(tag) })
                }
            }
            if (cursor != null) TextButton(onClick = { model.loadTags(true) }, enabled = !busy) { Text("更多标签") }
            Row(verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(extra, { extra = it }, label = { Text("新建标签") }, singleLine = true, modifier = Modifier.weight(1f))
                TextButton(onClick = { val value = extra.trim(); if (value.isNotEmpty()) model.createTag(value) { created ->
                    added = added + created; selected = (selected + created).distinct(); extra = ""
                } }, enabled = !busy) { Text("添加") }
            }
            message?.let { ErrorNotice(it, model::refreshTagsState) }
            if (baseRevision != asset.tagsRevision) {
                Text("服务器标签已变化，请载入当前标签后再编辑。")
                TextButton(onClick = { selected = asset.tags; baseRevision = asset.tagsRevision }) { Text("载入服务器标签") }
            }
        } },
        confirmButton = { TextButton(onClick = { model.saveTags(selected, baseRevision, onClose) }, enabled = !busy && baseRevision == asset.tagsRevision) { Text("保存") } },
        dismissButton = { TextButton(onClick = onClose, enabled = !busy) { Text("取消") } })
}

private fun compareTags(left: String, right: String): Int {
    val parts = Regex("[0-9]+|[^0-9]+")
    val a = parts.findAll(left).map { it.value }.toList()
    val b = parts.findAll(right).map { it.value }.toList()
    val collator = Collator.getInstance(Locale.CHINA)
    for (i in 0 until minOf(a.size, b.size)) {
        val x = a[i]; val y = b[i]
        val result = if (x[0].isDigit() && y[0].isDigit()) {
            val n = x.trimStart('0').ifEmpty { "0" }; val m = y.trimStart('0').ifEmpty { "0" }
            n.length.compareTo(m.length).takeIf { it != 0 } ?: n.compareTo(m)
        } else collator.compare(x, y)
        if (result != 0) return result
    }
    return a.size.compareTo(b.size).takeIf { it != 0 } ?: left.compareTo(right)
}

@Composable
fun TextEditDialog(title: String, initial: String, busy: Boolean, onClose: () -> Unit, onSave: (String) -> Unit) {
    var text by rememberSaveable { mutableStateOf(initial) }
    AlertDialog(onDismissRequest = { if (!busy) onClose() }, title = { Text(title) },
        text = { OutlinedTextField(text, { text = it }, modifier = Modifier.fillMaxWidth(), minLines = 3, maxLines = 8) },
        confirmButton = { TextButton(onClick = { onSave(text) }, enabled = !busy) { Text("保存") } },
        dismissButton = { TextButton(onClick = onClose, enabled = !busy) { Text("取消") } })
}

fun formatBytes(bytes: Long): String = when {
    bytes >= 1024L * 1024 * 1024 -> "%.1f GiB".format(bytes / (1024.0 * 1024 * 1024))
    bytes >= 1024 * 1024 -> "%.1f MiB".format(bytes / (1024.0 * 1024))
    bytes >= 1024 -> "%.1f KiB".format(bytes / 1024.0)
    else -> "$bytes B"
}
