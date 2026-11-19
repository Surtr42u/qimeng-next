package media.qimeng.next.screen

import androidx.compose.foundation.background
import androidx.activity.compose.BackHandler
import androidx.activity.compose.LocalActivity
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import coil3.compose.AsyncImage
import coil3.request.CachePolicy
import coil3.request.ImageRequest
import coil3.size.Size
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Asset
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.theme.isQimengDarkTheme
import java.text.Collator
import java.util.Locale

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DetailScreen(model: LibraryViewModel, id: String, target: Session?) {
    val asset by model.asset.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val assetAuthors by model.assetAuthors.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    var chromeVisible by rememberSaveable(id) { mutableStateOf(true) }
    var zoomImmersive by rememberSaveable(id) { mutableStateOf(false) }
    var resetImage by rememberSaveable(id) { mutableIntStateOf(0) }
    var playerActive by rememberSaveable(id) { mutableStateOf(false) }
    var playbackChrome by rememberSaveable(id) { mutableStateOf(true) }
    var showAuthorSheet by rememberSaveable(id) { mutableStateOf(false) }
    var showEditor by rememberSaveable(id) { mutableStateOf(false) }
    var showTags by rememberSaveable(id) { mutableStateOf(false) }
    var showNote by rememberSaveable(id) { mutableStateOf(false) }
    var showInfo by rememberSaveable(id) { mutableStateOf(false) }
    var showRename by rememberSaveable(id) { mutableStateOf(false) }
    var showTrash by rememberSaveable(id) { mutableStateOf(false) }
    var showAuthors by rememberSaveable(id) { mutableStateOf(false) }
    var showRelations by rememberSaveable(id) { mutableStateOf(false) }
    var showAcquisition by rememberSaveable(id) { mutableStateOf(false) }
    LaunchedEffect(id) { if (asset?.id != id) model.refreshAsset(id) }
    val item = asset?.takeIf { it.id == id && sameTarget(it.target, target) }
    if (item == null) {
        Box(Modifier.fillMaxSize()) {
            DetailTopChrome(true, -1, 0, model::back, {})
            Column(Modifier.align(Alignment.Center).padding(16.dp)) {
                message?.let { ErrorNotice(it) }
                TextButton(onClick = { model.refreshAsset(id) }, enabled = !busy) { Text("正在读取详情 · 点击重试") }
            }
        }
        return
    }
    TrackDwell(item, model)
    val visible = chromeVisible && !zoomImmersive && !playerActive
    DetailSystemBars(if (playerActive) playbackChrome else !zoomImmersive, lightIcons = playerActive || !visible)
    BackHandler(item.mediaType != "video" && (!chromeVisible || zoomImmersive)) { resetImage++; chromeVisible = true }
    val position = model.detailPosition(id)
    val backdrop = if (visible) MaterialTheme.colorScheme.background else Color.Black
    Box(Modifier.fillMaxSize().background(backdrop)) {
        if (item.mediaType == "video") VideoStage(item, model, Modifier.fillMaxSize(), { playerActive = it }, { playbackChrome = it })
        else ImageStage(item, model::adjacentAsset, { model.refreshAsset(item.id) }, Modifier.fillMaxSize(), { if (!zoomImmersive) chromeVisible = !chromeVisible }, { zoomImmersive = it }, resetImage)
        DetailTopChrome(visible, position.first, position.second, model::back, { showInfo = true }, Modifier.align(Alignment.TopCenter))
        DetailBottomChrome(visible, item, busy, busy, { model.setTodayLike(item, !item.liked) },
            { model.setFavorite(item, !item.favorite) }, { showTags = true; model.loadTags() }, { showAuthorSheet = true }, Modifier.align(Alignment.BottomCenter))
    }
    if (showAuthorSheet) DetailAuthorSheet(assetAuthors, busy,
        { id -> assetAuthors.find { it.id == id }?.let(model::followAssetAuthor) }, { author, _ -> model.openAuthor(author) },
        { showEditor = true }, { showAuthorSheet = false })
    if (showEditor) ModalBottomSheet(onDismissRequest = { showEditor = false }) {
        Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(item.name, style = MaterialTheme.typography.titleLarge)
            item.collectionId?.let { collection -> TextButton(onClick = { model.navigate("collection:$collection") }) { Text("同组作品${item.page?.let { " · 第 $it 项" }.orEmpty()}") } }
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
            message?.let { ErrorNotice(it) }
        }
    }
    if (showTags) TagsDialog(item, model) { showTags = false }
    if (showAuthors) AuthorsDialog(item, model) { showAuthors = false }
    if (showRelations) AssetRelationsDialog(item, model) { showRelations = false }
    if (showAcquisition) {
        val revision = rememberSaveable(item.id) { item.revision }
        TextEditDialog("获取渠道 / 来源（每行一条）", item.acquisitionSources.joinToString("\n"), busy, { showAcquisition = false }) {
            model.saveAcquisitionSources(item.copy(revision = revision), it.lines().filter(String::isNotBlank)) { showAcquisition = false }
        }
    }
    if (showNote) {
        val revision = rememberSaveable(item.id) { item.revision }
        TextEditDialog("编辑备注", item.note, busy, { showNote = false }) { value -> model.saveNote(item.copy(revision = revision), value) { showNote = false } }
    }
    if (showRename) {
        val revision = rememberSaveable(item.id) { item.revision }
        TextEditDialog("库内路径与文件名", item.relativePath, busy, { showRename = false }) { value -> model.renameCurrentAsset(item.copy(revision = revision), value) { showRename = false } }
    }
    if (showTrash) {
      val revision = rememberSaveable(item.id) { item.revision }
      AlertDialog(onDismissRequest = { showTrash = false }, title = { Text("移到回收站？") }, text = { Text(item.name) },
        confirmButton = { TextButton(onClick = { model.trashCurrentAsset(item.copy(revision = revision)) }, enabled = !busy) { Text("移到回收站") } },
        dismissButton = { TextButton(onClick = { showTrash = false }) { Text("取消") } })
    }
    if (showInfo) DetailInfoSheet(item) { showInfo = false }
}

@Composable
private fun AuthorsDialog(asset: Asset, model: LibraryViewModel, onClose: () -> Unit) {
    val state by model.authors.collectAsStateWithLifecycle()
    val associated by model.assetAuthors.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    val revision = rememberSaveable(asset.id) { asset.revision }
    val authorRevision = rememberSaveable(asset.id) { asset.authorsRevision }
    var selected by rememberSaveable(asset.id) { mutableStateOf(asset.authorIds) }
    var names by rememberSaveable(asset.id) { mutableStateOf("") }
    AlertDialog(onDismissRequest = { if (!busy) onClose() }, title = { Text("关联作者 · ${selected.size}") },
        text = { Column(Modifier.heightIn(max = 460.dp)) {
            LazyColumn(Modifier.weight(1f, fill = false)) {
                items(associated.filter { it.kind == "cos" }, key = { it.id }) { author -> Text("${author.name} · COS 目录作者") }
                items(state.items.filter { it.kind == "regular" && it.id != "unknown" }, key = { it.id }) { author ->
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(author.id in selected, { checked -> selected = if (checked) selected + author.id else selected - author.id })
                        Text(author.name)
                    }
                }
                if (state.cursor != null) item { TextButton(onClick = { model.loadAuthors(true) }, enabled = !state.loading) { Text("加载更多作者") } }
            }
            OutlinedTextField(names, { names = it }, label = { Text("新增作者名称（每行一个）") }, enabled = !busy, modifier = Modifier.fillMaxWidth())
            state.error?.let { ErrorNotice(it) { model.loadAuthors() } }
            message?.let { ErrorNotice(it) }
        } }, confirmButton = { TextButton(onClick = { model.saveAuthors(asset, selected.filterNot { it == "unknown" },
            names.lines().map(String::trim).filter(String::isNotEmpty).distinct(), revision, authorRevision, onClose) },
            enabled = !busy && !state.loading) { Text("保存") } },
        dismissButton = { TextButton(onClick = onClose, enabled = !busy) { Text("取消") } })
}

@Composable
private fun ImageStage(asset: Asset, onNavigate: (Int) -> Unit, onReload: () -> Unit, modifier: Modifier,
    onToggleChrome: () -> Unit, onZoom: (Boolean) -> Unit, reset: Int) {
    var scale by rememberSaveable(asset.id, asset.version) { mutableFloatStateOf(1f) }
    var panX by rememberSaveable(asset.id, asset.version) { mutableFloatStateOf(0f) }
    var panY by rememberSaveable(asset.id, asset.version) { mutableFloatStateOf(0f) }
    val context = LocalContext.current
    var error by remember(asset.id) { mutableStateOf(false) }
    var retry by remember(asset.id) { mutableIntStateOf(0) }
    LaunchedEffect(scale > 1f) { onZoom(scale > 1f) }
    LaunchedEffect(reset) { if (reset > 0) { scale = 1f; panX = 0f; panY = 0f } }
    Box(modifier
        .pointerInput(asset.id) { detectTapGestures(onTap = { onToggleChrome() }, onDoubleTap = { scale = if (scale > 1f) 1f else 2.5f; panX = 0f; panY = 0f }) }
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
                        panX = (panX + pan.x).coerceIn(-maxX, maxX)
                        panY = (panY + pan.y).coerceIn(-maxY, maxY)
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
                .diskCachePolicy(CachePolicy.DISABLED).memoryCacheKey("${asset.target.baseUrl}:${asset.target.instanceId}:${asset.id}:${asset.version}:original").build()
        }, imageLoader = LocalMediaImages.current, contentDescription = asset.name, contentScale = ContentScale.Fit,
            onError = { error = true }, onSuccess = { error = false },
            modifier = Modifier.fillMaxSize().graphicsLayer { scaleX = scale; scaleY = scale; translationX = panX; translationY = panY })
        if (error) FilledTonalButton(onClick = { retry++; error = false; onReload() }) { Text("图片加载失败 · 重试") }
    }
}

@Composable
private fun DetailSystemBars(visible: Boolean, lightIcons: Boolean) {
    val window = LocalActivity.current?.window ?: return
    val view = LocalView.current
    val dark = isQimengDarkTheme()
    DisposableEffect(window) {
        onDispose { WindowCompat.getInsetsController(window, view).show(WindowInsetsCompat.Type.systemBars()) }
    }
    SideEffect {
        WindowCompat.getInsetsController(window, view).apply {
            systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            isAppearanceLightStatusBars = !dark && !lightIcons
            isAppearanceLightNavigationBars = !dark && !lightIcons
            if (visible) show(WindowInsetsCompat.Type.systemBars()) else hide(WindowInsetsCompat.Type.systemBars())
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
    var added by rememberSaveable(asset.id) { mutableStateOf(emptyList<String>()) }
    val candidates = remember(vocabulary, asset.tags, added) { (vocabulary + asset.tags + added).distinct().sortedWith(::compareTags) }
    DetailTagManageSheet(pool = candidates, selectedTagIds = selected, savingTags = busy, saveEnabled = baseRevision == asset.tagsRevision,
        onToggleTag = { tag -> if (!busy) selected = if (tag in selected) selected - tag else selected + tag },
        onUnbindTag = { tag ->
            if (!busy) {
                if (tag in asset.tags) {
                    val baseWasCurrent = baseRevision == asset.tagsRevision
                    model.removeTag(tag) { updated -> selected = selected - tag; if (baseWasCurrent) baseRevision = updated.tagsRevision }
                } else selected = selected - tag
            }
        }, onCreateTag = { value, created ->
            if (!busy && value.isNotBlank()) model.createTag(value.trim()) { result ->
                added = added + result; selected = (selected + result).distinct(); created()
            }
        }, onSave = { model.saveTags(selected, baseRevision, onClose) }, onDismiss = { if (!busy) onClose() },
        poolFooter = { if (cursor != null) TextButton(onClick = { model.loadTags(true) }, enabled = !busy) { Text("更多标签") } },
        feedback = {
            message?.let { ErrorNotice(it, model::refreshTagsState) }
            if (baseRevision != asset.tagsRevision) {
                Text("服务器标签已变化，请载入当前标签后再编辑。")
                TextButton(onClick = { selected = asset.tags; baseRevision = asset.tagsRevision }) { Text("载入服务器标签") }
            }
        })
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
