package media.qimeng.next.screen

import androidx.compose.foundation.background
import androidx.activity.compose.BackHandler
import androidx.activity.compose.LocalActivity
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import coil3.network.HttpException
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Asset
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget
import media.qimeng.next.screen.video.VideoPlaybackSystemBars
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.theme.QimengDimens
import media.qimeng.next.ui.theme.isQimengDarkTheme
import java.io.IOException
import java.text.Collator
import java.util.Locale

@OptIn(ExperimentalMaterial3Api::class, ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun DetailScreen(model: LibraryViewModel, id: String, target: Session?) {
    val asset by model.asset.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val assetAuthors by model.assetAuthors.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    val pendingTools by model.pendingAssetTools.collectAsStateWithLifecycle()
    var chromeVisible by rememberSaveable(id) { mutableStateOf(true) }
    var zoomImmersive by rememberSaveable(id) { mutableStateOf(false) }
    var resetImage by rememberSaveable(id) { mutableIntStateOf(0) }
    var playerActive by remember(id) { mutableStateOf(false) }
    var playbackChrome by remember(id) { mutableStateOf(true) }
    var showAuthorSheet by rememberSaveable(id) { mutableStateOf(false) }
    var showEditor by rememberSaveable(id) { mutableStateOf(false) }
    var showTags by rememberSaveable(id) { mutableStateOf(false) }
    var showNote by rememberSaveable(id) { mutableStateOf(false) }
    var showInfo by rememberSaveable(id) { mutableStateOf(false) }
    var showRename by rememberSaveable(id) { mutableStateOf(false) }
    var showTrash by rememberSaveable(id) { mutableStateOf(false) }
    var showRelations by rememberSaveable(id) { mutableStateOf(false) }
    var showAcquisition by rememberSaveable(id) { mutableStateOf(false) }
    LaunchedEffect(id, pendingTools) {
        if (pendingTools == id) {
            showEditor = true
            model.consumeAssetTools(id)
        }
    }
    LaunchedEffect(id) { if (asset?.id != id) model.refreshAsset(id) }
    val item = asset?.takeIf { it.id == id && sameTarget(it.target, target) }
    if (item == null) {
        // 深链 / 任务跳转落地时详情尚未到达：先铺终态几何（全屏舞台 + 返回顶栏），
        // 只在舞台中央放加载指示，等 item 到达后原地换内容，避免先空白再整块弹入。
        val notice = message
        Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
            DetailTopChrome(true, -1, 0, model::back, {})
            if (notice == null) {
                LoadingIndicator(
                    modifier = Modifier
                        .align(Alignment.Center)
                        .size(QimengDimens.PageLoadingIndicatorSize)
                        .testTag("asset-detail-loading"),
                )
            } else {
                Column(Modifier.align(Alignment.Center).padding(16.dp)) {
                    ErrorNotice(notice)
                    TextButton(onClick = { model.refreshAsset(id) }, enabled = !busy) { Text("点击重试") }
                }
            }
        }
        return
    }
    TrackDwell(item, model)
    val visible = chromeVisible && !zoomImmersive && !playerActive
    DetailSystemBars(if (playerActive) playbackChrome else !zoomImmersive, lightIcons = playerActive || !visible, videoPlayback = playerActive)
    BackHandler(item.mediaType != "video" && (!chromeVisible || zoomImmersive)) { resetImage++; chromeVisible = true }
    val position = model.detailPosition(id)
    val backdrop = if (visible) MaterialTheme.colorScheme.background else Color.Black
    Box(Modifier.fillMaxSize().background(backdrop).testTag("asset-detail")) {
        if (item.mediaType == "video") {
            VideoStage(item, model, Modifier.fillMaxSize(), { playerActive = it }, { playbackChrome = it })
        } else {
            ZoomableImageStage(
                asset = item,
                onNavigate = model::adjacentAsset,
                onReload = { model.refreshAsset(item.id) },
                modifier = Modifier.fillMaxSize(),
                onToggleChrome = { if (!zoomImmersive) chromeVisible = !chromeVisible },
                onZoomImmersive = { zoomImmersive = it },
                reset = resetImage,
            )
            // 视频播放时不预载：避免邻位原图下载和视频流抢带宽（旧版 preloadEnabled 同口径）。
            DetailNeighbourOriginalPreload(model, item)
        }
        DetailTopChrome(visible, position.first, position.second, model::back, { showInfo = true }, Modifier.align(Alignment.TopCenter))
        DetailBottomChrome(visible, item, busy, busy, { model.setTodayLike(item, !item.liked) },
            { model.setFavorite(item, !item.favorite) }, { showTags = true; model.loadTags() }, { showAuthorSheet = true }, Modifier.align(Alignment.BottomCenter))
    }
    if (showAuthorSheet) DetailAuthorSheet(assetAuthors, busy,
        { id -> assetAuthors.find { it.id == id }?.let(model::followAssetAuthor) },
        // 作者卡已经持有该作者的名字与 fileCount：seed 进作者作品页，标题/计数首帧即真值。
        { authorId, _ -> model.openAuthor(authorId, seed = assetAuthors.find { it.id == authorId }) },
        { model.openAssetEditor(item) }, { showAuthorSheet = false })
    if (showEditor) ModalBottomSheet(onDismissRequest = { showEditor = false }) {
        Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(item.name, style = MaterialTheme.typography.titleLarge)
            item.collectionId?.let { collection -> TextButton(onClick = { model.navigate("collection:$collection") }) { Text("同组作品${item.page?.let { " · 第 $it 项" }.orEmpty()}") } }
            TextButton(onClick = { showEditor = false; model.openAssetEditor(item) }) { Text("修改关联作者") }
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
private fun DetailSystemBars(visible: Boolean, lightIcons: Boolean, videoPlayback: Boolean) {
    val window = LocalActivity.current?.window ?: return
    val view = LocalView.current
    val dark = isQimengDarkTheme()
    if (videoPlayback) VideoPlaybackSystemBars(window, view, visible)
    DisposableEffect(window) {
        onDispose { WindowCompat.getInsetsController(window, view).show(WindowInsetsCompat.Type.systemBars()) }
    }
    SideEffect {
        WindowCompat.getInsetsController(window, view).apply {
            systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            isAppearanceLightStatusBars = !dark && !lightIcons
            isAppearanceLightNavigationBars = !dark && !lightIcons
            if (!videoPlayback) {
                if (visible) show(WindowInsetsCompat.Type.systemBars()) else hide(WindowInsetsCompat.Type.systemBars())
            }
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

/** 失败归因的 cause 链最大深度：防止自引用链死循环（旧版 ImageLoadErrorClassifier 同值）。 */
internal const val MAX_CAUSE_CHAIN_DEPTH = 5

internal enum class ImageFailureKind { NETWORK, DECODE }

/**
 * 原图加载失败归因（旧版 feature/detail ImageLoadErrorClassifier 两档口径）：
 * cause 链上出现 [IOException]（超时/断流/连接失败等）或 [HttpException]（非 2xx 响应，
 * 继承 RuntimeException 而非 IOException）→ 网络；其余（解码器拒绝、格式不支持等）→ 解码。
 * 归因决定详情覆盖层文案：把网络抖动报成「文件损坏」会误导用户。
 */
internal fun classifyImageFailure(cause: Throwable): ImageFailureKind {
    var current: Throwable? = cause
    var depth = 0
    while (current != null && depth < MAX_CAUSE_CHAIN_DEPTH) {
        if (current is IOException || current is HttpException) return ImageFailureKind.NETWORK
        current = current.cause
        depth++
    }
    return ImageFailureKind.DECODE
}

/** 文案与旧版 strings.xml 一致：传输类提示网络，其余提示文件无法解码。 */
internal fun imageFailureMessage(kind: ImageFailureKind): String = when (kind) {
    ImageFailureKind.NETWORK -> "网络不畅，图片加载失败，请重试"
    ImageFailureKind.DECODE -> "该文件无法解码，可能已损坏或格式不受支持"
}
