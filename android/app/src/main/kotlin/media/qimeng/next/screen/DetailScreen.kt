package media.qimeng.next.screen

import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.*
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

@Composable
fun DetailScreen(model: LibraryViewModel, id: String) {
    val asset by model.asset.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    var showTags by rememberSaveable(id) { mutableStateOf(false) }
    var showNote by rememberSaveable(id) { mutableStateOf(false) }
    var showInfo by rememberSaveable(id) { mutableStateOf(false) }
    var showRename by rememberSaveable(id) { mutableStateOf(false) }
    var showTrash by rememberSaveable(id) { mutableStateOf(false) }
    LaunchedEffect(id) { if (asset?.id != id) model.openAsset(id) }
    val item = asset?.takeIf { it.id == id }
    if (item == null) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { TextButton(onClick = { model.openAsset(id) }, enabled = !busy) { Text("正在读取详情 · 点击重试") } }
        return
    }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).navigationBarsPadding(), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (item.mediaType == "video") VideoStage(item, model) else ImageStage(item)
        ContentCard(Modifier.padding(horizontal = 16.dp)) {
            Text(item.name, style = MaterialTheme.typography.titleLarge)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(item.favorite, { model.changeReaction(!item.favorite, item.liked) }, label = { Text("收藏") }, enabled = !busy,
                    leadingIcon = { Icon(Icons.Outlined.Bookmark, null, Modifier.size(18.dp)) })
                FilterChip(item.liked, { model.changeReaction(item.favorite, !item.liked) }, label = { Text("喜欢") }, enabled = !busy,
                    leadingIcon = { Icon(Icons.Outlined.FavoriteBorder, null, Modifier.size(18.dp)) })
                TextButton(onClick = { showInfo = true }) { Text("详细信息") }
            }
            item.authorId?.let { authorId -> TextButton(onClick = { model.openAuthor(authorId) }) { Text(item.authorName?.takeIf(String::isNotBlank) ?: "查看作者档案") } }
            Text("标签", style = MaterialTheme.typography.titleSmall)
            Text(if (item.tags.isEmpty()) "暂无标签" else item.tags.joinToString(" · "), color = MaterialTheme.colorScheme.onSurfaceVariant)
            TextButton(onClick = { showTags = true; model.loadTags() }) { Text("管理标签") }
            Text("备注", style = MaterialTheme.typography.titleSmall)
            Text(item.note.ifBlank { "暂无备注" })
            TextButton(onClick = { showNote = true }) { Text("编辑备注") }
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
    if (showNote) TextEditDialog("编辑备注", item.note, busy, { showNote = false }) { value -> model.saveNote(value) { showNote = false } }
    if (showRename) TextEditDialog("库内路径与文件名", item.name, busy, { showRename = false }) { value -> model.renameCurrentAsset(value) { showRename = false } }
    if (showTrash) AlertDialog(onDismissRequest = { showTrash = false }, title = { Text("移到回收站？") }, text = { Text(item.name) },
        confirmButton = { TextButton(onClick = { showTrash = false; model.trashCurrentAsset() }, enabled = !busy) { Text("移到回收站") } },
        dismissButton = { TextButton(onClick = { showTrash = false }) { Text("取消") } })
    if (showInfo) {
        val clipboard = LocalClipboardManager.current
        var copied by remember { mutableStateOf(false) }
        AlertDialog(onDismissRequest = { showInfo = false }, title = { Text("详细信息") },
            text = { Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(item.name)
                Text("${item.mediaType} · ${formatBytes(item.bytes)}")
                Text("入库时间：${item.addedAt}")
                Text("内容版本 ${item.version}")
                TextButton(onClick = { clipboard.setText(AnnotatedString(item.name)); copied = true }) { Text(if (copied) "已复制文件名" else "复制完整文件名") }
            } }, confirmButton = { TextButton(onClick = { showInfo = false }) { Text("关闭") } })
    }
}

@Composable
private fun ImageStage(asset: Asset) {
    var scale by rememberSaveable(asset.id, asset.version) { mutableFloatStateOf(1f) }
    var offset by remember { mutableStateOf(Offset.Zero) }
    val context = LocalContext.current
    var error by remember(asset.id) { mutableStateOf(false) }
    var retry by remember(asset.id) { mutableIntStateOf(0) }
    Box(Modifier.fillMaxWidth().heightIn(min = 280.dp, max = 620.dp).aspectRatio(0.85f).background(Color.Black)
        .pointerInput(asset.id) { detectTapGestures(onDoubleTap = { scale = if (scale > 1f) 1f else 2.5f; offset = Offset.Zero }) }
        .pointerInput(asset.id) { detectTransformGestures { _, pan, zoom, _ ->
            scale = (scale * zoom).coerceIn(1f, 6f)
            val maxX = size.width * (scale - 1f) / 2
            val maxY = size.height * (scale - 1f) / 2
            offset = Offset((offset.x + pan.x).coerceIn(-maxX, maxX), (offset.y + pan.y).coerceIn(-maxY, maxY))
        } }, contentAlignment = Alignment.Center) {
        AsyncImage(model = remember(asset.contentUrl, asset.version, retry) {
            ImageRequest.Builder(context).data(asset.contentUrl).size(Size.ORIGINAL)
                .diskCachePolicy(CachePolicy.DISABLED).memoryCacheKey("${asset.contentUrl}:${asset.version}").build()
        }, imageLoader = LocalMediaImages.current, contentDescription = asset.name, contentScale = ContentScale.Fit,
            onError = { error = true }, onSuccess = { error = false },
            modifier = Modifier.fillMaxSize().graphicsLayer { scaleX = scale; scaleY = scale; translationX = offset.x; translationY = offset.y })
        if (error) FilledTonalButton(onClick = { retry++; error = false }) { Text("图片加载失败 · 重试") }
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
    val busy by model.busy.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    var selected by rememberSaveable(asset.id) { mutableStateOf(asset.tags) }
    var extra by rememberSaveable { mutableStateOf("") }
    var added by rememberSaveable { mutableStateOf(emptyList<String>()) }
    AlertDialog(onDismissRequest = { if (!busy) onClose() }, title = { Text("标签 · 已选 ${selected.size}") },
        text = { Column(Modifier.heightIn(max = 460.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(selected.joinToString(" · ").ifBlank { "尚未选择标签" }, Modifier.heightIn(min = 60.dp, max = 76.dp).verticalScroll(rememberScrollState()))
            FlowRow(Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                (vocabulary + asset.tags + added).distinct().sorted().forEach { tag ->
                    FilterChip(tag in selected, { selected = if (tag in selected) selected - tag else selected + tag }, label = { Text(tag) })
                }
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(extra, { extra = it }, label = { Text("新建标签") }, singleLine = true, modifier = Modifier.weight(1f))
                TextButton(onClick = { val value = extra.trim(); if (value.isNotEmpty()) { added = added + value; selected = (selected + value).distinct(); extra = "" } }) { Text("添加") }
            }
            message?.let { ErrorNotice(it) }
        } },
        confirmButton = { TextButton(onClick = { model.saveTags(selected, onClose) }, enabled = !busy) { Text("保存") } },
        dismissButton = { TextButton(onClick = onClose, enabled = !busy) { Text("取消") } })
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
