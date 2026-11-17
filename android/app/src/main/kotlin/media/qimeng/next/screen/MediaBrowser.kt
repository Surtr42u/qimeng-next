package media.qimeng.next.screen

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.grid.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil3.compose.AsyncImage
import coil3.request.ImageRequest
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Asset
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice

@Composable
fun HomeScreen(model: LibraryViewModel) {
    var mode by rememberSaveable { mutableStateOf(0) }
    var period by rememberSaveable { mutableStateOf("week") }
    var seeds by rememberSaveable { mutableStateOf(listOf(1L, 2L)) }
    val states = rememberSaveableStateHolder()
    val capabilities by model.capabilities.collectAsStateWithLifecycle()
    Column {
        PageHeader("绮梦影库", actions = {
            IconButton(onClick = { model.navigate("search") }) { Icon(Icons.Outlined.Search, "搜索") }
            IconButton(onClick = { model.navigate("upload") }) { Icon(Icons.Outlined.Upload, "上传") }
        })
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            listOf("推荐", "COS", "排行榜").forEachIndexed { index, label ->
                FilterChip(selected = mode == index, onClick = { mode = index }, label = { Text(label) })
            }
        }
        if (mode == 2) Row(Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            listOf("day" to "日", "week" to "周", "month" to "月", "quarter" to "季", "year" to "年", "all" to "全部").forEach { (value, label) ->
                FilterChip(period == value, { period = value }, label = { Text(label) })
            }
        }
        if (capabilities == null) {
            LinearProgressIndicator(Modifier.fillMaxWidth())
        } else if ((mode != 2 && capabilities?.recommendations == false) || (mode == 2 && capabilities?.rankings == false)) {
            ContentCard(Modifier.padding(16.dp)) {
                Text(if (mode == 0) "推荐服务尚未就绪" else "排行榜服务尚未就绪", style = MaterialTheme.typography.titleMedium)
                Text("当前服务器未提供此能力。可在相册按影库与类型浏览。")
                OutlinedButton(onClick = { model.selectTab(1) }) { Text("浏览相册") }
            }
        } else states.SaveableStateProvider("home:$mode") {
            MediaBrowser(model, "home:$mode", kind = if (mode == 1) "cos" else null, bottomDock = true,
                source = if (mode == 2) "rankings" else "recommendations", seed = seeds.getOrElse(mode) { 1L }, period = period,
                onRefresh = if (mode != 2) ({ seeds = seeds.mapIndexed { index, value -> if (index == mode) kotlin.random.Random.nextLong(1, Long.MAX_VALUE) else value } }) else null)
        }
    }
}

@Composable
fun AlbumsScreen(model: LibraryViewModel) {
    Column {
        PageHeader("相册", actions = { IconButton(onClick = { model.navigate("search") }) { Icon(Icons.Outlined.Search, "搜索相册") } })
        MediaBrowser(model, "albums", bottomDock = true, libraryFilter = true)
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MediaBrowser(model: LibraryViewModel, key: String, search: Boolean = false, favorite: Boolean? = null,
                 kind: String? = null, author: String? = null, bottomDock: Boolean = false, libraryFilter: Boolean = false,
                 source: String = "assets", seed: Long = 1, period: String = "week", onRefresh: (() -> Unit)? = null) {
    val state by model.assets(key).collectAsStateWithLifecycle()
    val libraries by model.libraries.collectAsStateWithLifecycle()
    var draft by rememberSaveable(key) { mutableStateOf(model.query(key).text) }
    var submitted by rememberSaveable(key) { mutableStateOf(model.query(key).text) }
    var type by rememberSaveable(key) { mutableStateOf<String?>(null) }
    var partition by rememberSaveable(key) { mutableStateOf(kind) }
    var library by rememberSaveable(key) { mutableStateOf<String?>(null) }
    var sort by rememberSaveable(key) { mutableStateOf("added_desc") }
    var columns by rememberSaveable(key) { mutableIntStateOf(3) }
    var showFilters by rememberSaveable(key) { mutableStateOf(false) }
    var tag by rememberSaveable(key) { mutableStateOf("") }
    val grid = rememberLazyGridState()
    val query = AssetQuery(text = submitted, mediaType = type, kind = partition, favorite = favorite, authorId = author,
        libraryId = library, tag = tag.takeIf(String::isNotBlank), sort = sort,
        source = if (library != null || tag.isNotBlank() || sort != "added_desc") "assets" else source, seed = seed, period = period)
    var appliedQuery by rememberSaveable(key) { mutableStateOf(query.toString()) }
    LaunchedEffect(key, query) {
        model.browse(key, query)
        if (appliedQuery != query.toString()) { grid.scrollToItem(0); appliedQuery = query.toString() }
    }
    val lastVisible by remember { derivedStateOf { grid.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 } }
    LaunchedEffect(lastVisible, state.items.size, state.cursor) {
        if (state.items.isNotEmpty() && lastVisible >= state.items.size - 8) model.browse(key, query, next = true)
    }
    Column(Modifier.fillMaxSize()) {
        if (search) Row(Modifier.padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(draft, { draft = it }, label = { Text("文件名、作者或标签") }, singleLine = true,
                modifier = Modifier.weight(1f), trailingIcon = { IconButton(onClick = { submitted = draft.trim(); type = null; partition = null }) { Icon(Icons.Outlined.Search, "提交搜索") } })
        }
        Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (source != "rankings") {
                listOf(null to "综合", "video" to "视频", "animated_image" to "动图", "image" to "图片").forEach { (value, label) ->
                    FilterChip(type == value, { type = value }, label = { Text(label) })
                }
                IconButton(onClick = { showFilters = !showFilters }) { Icon(Icons.Outlined.Tune, "筛选与排序") }
            }
            IconButton(onClick = { columns = if (columns == 5) 2 else columns + 1 }) { Icon(Icons.Outlined.GridView, "网格 $columns 列，切换列数") }
            IconButton(onClick = { if (query.source == "recommendations" && onRefresh != null) onRefresh() else model.browse(key, query, force = true) }) { Icon(Icons.Outlined.Refresh, "刷新") }
        }
        if (search || source == "rankings") Row(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            listOf(null to "全部", "regular" to "常规", "cos" to "COS").forEach { (value, label) ->
                FilterChip(partition == value, { partition = value }, label = { Text(label) })
            }
        }
        if (libraryFilter || showFilters) Row(Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(library == null, { library = null }, label = { Text("全部影库") })
            libraries.forEach { item -> FilterChip(library == item.id, { library = item.id }, label = { Text(item.name) }) }
        }
        if (showFilters) Column(Modifier.padding(horizontal = 16.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf("added_desc" to "最新入库", "name_asc" to "名称", "updated_desc" to "最近修改").forEach { (value, label) ->
                    FilterChip(sort == value, { sort = value }, label = { Text(label) })
                }
            }
            OutlinedTextField(tag, { tag = it }, label = { Text("标签") }, singleLine = true, modifier = Modifier.fillMaxWidth())
        }
        state.error?.let { ErrorNotice(it) { model.browse(key, query, force = true) } }
        if (state.loading) LinearProgressIndicator(Modifier.fillMaxWidth())
        if (state.loaded && state.items.isEmpty() && !state.loading) Box(Modifier.weight(1f).fillMaxWidth(), contentAlignment = Alignment.Center) { Text("没有符合条件的媒体") }
        PullToRefreshBox(isRefreshing = state.loading && state.loaded,
            onRefresh = { if (query.source == "recommendations" && onRefresh != null) onRefresh() else model.browse(key, query, force = true) },
            modifier = Modifier.weight(1f)) {
        LazyVerticalGrid(columns = GridCells.Fixed(columns), state = grid,
            contentPadding = PaddingValues(start = 12.dp, end = 12.dp, top = 12.dp, bottom = if (bottomDock) 128.dp else 24.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(12.dp),
            modifier = Modifier.fillMaxSize().pointerInput(key) {
                awaitEachGesture {
                    awaitFirstDown(requireUnconsumed = false)
                    var accumulated = 1f
                    do {
                        val event = awaitPointerEvent()
                        if (event.changes.count { it.pressed } >= 2) {
                            accumulated *= event.calculateZoom()
                            if (accumulated > 1.22f) { columns = (columns - 1).coerceAtLeast(2); accumulated = 1f }
                            if (accumulated < 0.82f) { columns = (columns + 1).coerceAtMost(5); accumulated = 1f }
                            event.changes.forEach { it.consume() }
                        }
                    } while (event.changes.any { it.pressed })
                }
            }) {
            items(state.items, key = Asset::id) { asset -> MediaTile(asset, search) { model.openAsset(asset.id, key) } }
            if (state.cursor != null) item(span = { GridItemSpan(maxLineSpan) }) {
                TextButton(onClick = { model.browse(key, query, next = true) }, enabled = !state.loading, modifier = Modifier.fillMaxWidth()) { Text("加载更多") }
            }
        }
        }
    }
}

@Composable
private fun MediaTile(asset: Asset, shortTitle: Boolean, onClick: () -> Unit) {
    val context = LocalContext.current
    Column(Modifier.clip(RoundedCornerShape(12.dp)).clickable(onClick = onClick).semantics { contentDescription = asset.name }) {
        Box(Modifier.fillMaxWidth().aspectRatio(0.76f).clip(RoundedCornerShape(12.dp)).background(MaterialTheme.colorScheme.surfaceVariant)) {
            if (asset.thumbnailUrl.isNotEmpty()) {
                AsyncImage(model = ImageRequest.Builder(context).data(asset.thumbnailUrl)
                    .memoryCacheKey("${asset.thumbnailUrl}:${asset.version}").build(), imageLoader = LocalMediaImages.current,
                    contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            }
            else Icon(Icons.Outlined.Image, "预览尚未生成", Modifier.align(Alignment.Center).size(28.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            if (asset.mediaType == "video") Icon(Icons.Outlined.PlayCircle, "视频", Modifier.align(Alignment.Center).size(32.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            if (asset.favorite) Icon(Icons.Outlined.Bookmark, "已收藏", Modifier.align(Alignment.TopEnd).padding(6.dp).size(18.dp), tint = MaterialTheme.colorScheme.primary)
        }
        Text(if (shortTitle) asset.name.substringBeforeLast('.', asset.name) else asset.name,
            Modifier.padding(top = 5.dp, start = 2.dp, end = 2.dp), maxLines = if (shortTitle) 1 else 2,
            overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.labelSmall)
        asset.rankingScore?.let { Text("浏览 $it", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}
