package media.qimeng.next.screen

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.grid.*
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
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
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil3.compose.AsyncImage
import coil3.request.ImageRequest
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Asset
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.QimengChipRow
import media.qimeng.next.ui.component.QimengPill
import media.qimeng.next.ui.glass.GlassSurface
import media.qimeng.next.ui.glass.GlassIconButton
import media.qimeng.next.ui.icon.*
import media.qimeng.next.ui.theme.QimengShapes
import media.qimeng.next.ui.theme.softDaylightShadow
import kotlinx.coroutines.launch

@Composable
fun HomeScreen(model: LibraryViewModel) {
    var period by rememberSaveable { mutableStateOf("day") }
    var seeds by rememberSaveable { mutableStateOf(listOf(1L, 2L)) }
    val pager = rememberPagerState { 3 }
    val scope = rememberCoroutineScope()
    val preferences by model.preferences.collectAsStateWithLifecycle()
    val regular = rememberBrowserControls("home:0", columns = 2)
    val cos = rememberBrowserControls("home:1", "cos", columns = 2)
    val rank = rememberBrowserControls("home:2", columns = 2)
    val controls = listOf(regular, cos, rank)
    val mode = pager.targetPage
    val current = controls[mode]
    val capabilities by model.capabilities.collectAsStateWithLifecycle()
    Column(Modifier.fillMaxSize()) {
        HomeTopRow(current.columns.intValue, { model.navigate("search") }, { current.showFilters.value = true }) {
            val next = if (current.columns.intValue == 2) 1 else 2
            controls.forEach { it.columns.intValue = next }
        }
        QimengChipRow(listOf("推荐", "COS", "排行榜").mapIndexed { index, label -> QimengPill(label, mode == index) },
            onPillClick = { page -> scope.launch {
                if (preferences.reducedMotion || !android.animation.ValueAnimator.areAnimatorsEnabled()) pager.scrollToPage(page) else pager.animateScrollToPage(page)
            } }, pillModifier = Modifier.softDaylightShadow(QimengShapes.pill), modifier = Modifier.padding(horizontal = 16.dp))
        if (mode == 2) {
            Spacer(Modifier.height(4.dp))
            val periods = listOf("day" to "日", "week" to "周", "month" to "月", "year" to "年")
            QimengChipRow(periods.map { QimengPill(it.second, period == it.first) }, { period = periods[it].first }, modifier = Modifier.padding(horizontal = 16.dp))
        }
        HorizontalPager(pager, modifier = Modifier.weight(1f), key = { "home:$it" }) { page ->
            if (capabilities == null) LinearProgressIndicator(Modifier.fillMaxWidth())
            else if ((page != 2 && capabilities?.recommendations == false) || (page == 2 && capabilities?.rankings == false)) {
                ContentCard(Modifier.padding(16.dp)) {
                    Text(if (page == 2) "排行榜服务尚未就绪" else "推荐服务尚未就绪")
                    OutlinedButton(onClick = model::refreshConnection) { Text("重试") }
                }
            } else {
                MediaBrowser(model, "home:$page", kind = if (page == 1) "cos" else null, bottomDock = true,
                    source = if (page == 2) "rankings" else "recommendations", seed = seeds.getOrElse(page) { 1L }, period = period,
                    onRefresh = if (page != 2) ({ seeds = seeds.mapIndexed { index, value -> if (index == page) kotlin.random.Random.nextLong(1, Long.MAX_VALUE) else value } }) else null,
                    controls = controls[page], toolbar = false, pinchEnabled = false)
            }
        }
    }
}

/** Existing HomeTopRow geometry and controls; callbacks now belong to the new client. */
@Composable
private fun HomeTopRow(columns: Int, onSearch: () -> Unit, onFilter: () -> Unit, onColumns: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Text("首页", style = MaterialTheme.typography.titleLarge.copy(fontSize = 24.sp, fontWeight = FontWeight.Bold), modifier = Modifier.padding(end = 10.dp))
        GlassSurface(shape = QimengShapes.pill, modifier = Modifier.weight(1f).height(40.dp).softDaylightShadow(QimengShapes.pill).clickable(onClick = onSearch)) {
            Box(Modifier.fillMaxHeight(), contentAlignment = Alignment.Center) {
                Text("搜索", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 12.dp))
            }
        }
        GlassIconButton(HomeFilterIcon, "筛选", onFilter, tint = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(start = 10.dp, end = 6.dp).softDaylightShadow(QimengShapes.pill))
        GlassIconButton(if (columns == 1) Grid1Icon else gridIconFor(columns), "切换列数", onColumns, tint = MaterialTheme.colorScheme.primary,
            modifier = Modifier.softDaylightShadow(QimengShapes.pill))
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
                 source: String = "assets", seed: Long = 1, period: String = "week", onRefresh: (() -> Unit)? = null, collectionId: String? = null, initialTag: String? = null,
                 controls: BrowserControls = rememberBrowserControls(key, kind, initialTag.orEmpty()), toolbar: Boolean = true, pinchEnabled: Boolean = true,
                 textOverride: String? = null, caption: Boolean = search) {
    val state by model.assets(key).collectAsStateWithLifecycle()
    val libraries by model.libraries.collectAsStateWithLifecycle()
    var draft by rememberSaveable(key) { mutableStateOf(model.query(key).text) }
    var submitted by rememberSaveable(key) { mutableStateOf(model.query(key).text) }
    var type by controls.type
    var partition by controls.partition
    var library by controls.library
    var sort by controls.sort
    var columns by controls.columns
    var showFilters by controls.showFilters
    var tag by controls.tag
    val grid = rememberLazyGridState()
    val query = AssetQuery(text = textOverride ?: submitted, mediaType = type, kind = partition, favorite = favorite, authorId = author,
        libraryId = library, tag = tag.takeIf(String::isNotBlank), sort = sort,
        source = if (library != null || tag.isNotBlank() || sort != "added_desc") "assets" else source, seed = seed, period = period, collectionId = collectionId)
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
        if (toolbar) Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (source != "rankings") {
                listOf(null to "综合", "video" to "视频", "animated_image" to "动图", "image" to "图片").forEach { (value, label) ->
                    FilterChip(type == value, { type = value }, label = { Text(label) })
                }
                IconButton(onClick = { showFilters = !showFilters }) { Icon(Icons.Outlined.Tune, "筛选与排序") }
            }
            IconButton(onClick = { columns = if (columns == 5) 2 else columns + 1 }) { Icon(Icons.Outlined.GridView, "网格 $columns 列，切换列数") }
            IconButton(onClick = { if (query.source == "recommendations" && onRefresh != null) onRefresh() else model.browse(key, query, force = true) }) { Icon(Icons.Outlined.Refresh, "刷新") }
        }
        if (toolbar && (search || source == "rankings")) Row(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            listOf(null to "全部", "regular" to "常规", "cos" to "COS").forEach { (value, label) ->
                FilterChip(partition == value, { partition = value }, label = { Text(label) })
            }
        }
        if (toolbar && libraryFilter) Row(Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(library == null, { library = null }, label = { Text("全部影库") })
            libraries.forEach { item -> FilterChip(library == item.id, { library = item.id }, label = { Text(item.name) }) }
        }
        if (showFilters) {
            var filterType by rememberSaveable(key) { mutableStateOf(type) }
            var filterPartition by rememberSaveable(key) { mutableStateOf(partition) }
            var filterLibrary by rememberSaveable(key) { mutableStateOf(library) }
            var filterSort by rememberSaveable(key) { mutableStateOf(sort) }
            var filterTag by rememberSaveable(key) { mutableStateOf(tag) }
            ModalBottomSheet(onDismissRequest = { showFilters = false }) { Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("筛选", style = MaterialTheme.typography.titleLarge)
            QimengChipRow(listOf(null to "全部", "image" to "图片", "animated_image" to "动图", "video" to "视频").map { QimengPill(it.second, filterType == it.first) }, { filterType = listOf(null, "image", "animated_image", "video")[it] })
            if (kind == null) QimengChipRow(listOf(null to "全部分区", "regular" to "常规", "cos" to "COS").map { QimengPill(it.second, filterPartition == it.first) }, { filterPartition = listOf(null, "regular", "cos")[it] })
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                media.qimeng.next.ui.component.QimengSegPill("全部影库", filterLibrary == null, { filterLibrary = null })
                libraries.forEach { entry -> media.qimeng.next.ui.component.QimengSegPill(entry.name, filterLibrary == entry.id, { filterLibrary = entry.id }) }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf("added_desc" to "最新入库", "name_asc" to "名称", "updated_desc" to "最近修改").forEach { (value, label) ->
                    FilterChip(filterSort == value, { filterSort = value }, label = { Text(label) })
                }
            }
            OutlinedTextField(filterTag, { filterTag = it }, label = { Text("标签") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedButton(onClick = { filterType = null; filterPartition = kind; filterLibrary = null; filterSort = "added_desc"; filterTag = initialTag.orEmpty() }) { Text("重置") }
                Button(onClick = { type = filterType; partition = filterPartition; library = filterLibrary; sort = filterSort; tag = filterTag; showFilters = false }) { Text("应用筛选") }
            }
        } } }
        state.error?.let { ErrorNotice(it) { model.browse(key, query, force = true) } }
        if (state.loading) LinearProgressIndicator(Modifier.fillMaxWidth())
        if (state.loaded && state.items.isEmpty() && !state.loading) Box(Modifier.weight(1f).fillMaxWidth(), contentAlignment = Alignment.Center) { Text("没有符合条件的媒体") }
        PullToRefreshBox(isRefreshing = state.loading && state.loaded,
            onRefresh = { if (query.source == "recommendations" && onRefresh != null) onRefresh() else model.browse(key, query, force = true) },
            modifier = Modifier.weight(1f).testTag("media-browser:$key")) {
        LazyVerticalGrid(columns = GridCells.Fixed(columns), state = grid,
            contentPadding = PaddingValues(start = 11.dp, end = 11.dp, top = 4.dp, bottom = if (bottomDock) 128.dp else 24.dp),
            horizontalArrangement = Arrangement.spacedBy(0.dp), verticalArrangement = Arrangement.spacedBy(0.dp),
            modifier = Modifier.fillMaxSize().pointerInput(key) {
                awaitEachGesture {
                    awaitFirstDown(requireUnconsumed = false)
                    var accumulated = 1f
                    do {
                        val event = awaitPointerEvent()
                        if (pinchEnabled && event.changes.count { it.pressed } >= 2) {
                            accumulated *= event.calculateZoom()
                            if (accumulated > 1.22f) { columns = (columns - 1).coerceAtLeast(2); accumulated = 1f }
                            if (accumulated < 0.82f) { columns = (columns + 1).coerceAtMost(5); accumulated = 1f }
                            event.changes.forEach { it.consume() }
                        }
                    } while (event.changes.any { it.pressed })
                }
            }) {
            items(if (model.query(key).text == query.text) state.items else emptyList(), key = Asset::id) { asset -> MediaTile(asset, caption) { model.openAsset(asset.id, key) } }
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
    val radius = with(LocalDensity.current) { 24f.toDp() }
    // Existing AssetCard: 16:9 frame, 5dp outer spacing and a 24px corner; only search adds a caption.
    Column {
        Box(Modifier.padding(5.dp).fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(radius))
            .background(MaterialTheme.colorScheme.surfaceVariant).clickable(onClick = onClick).semantics { contentDescription = asset.name }) {
            if (asset.thumbnailUrl.isNotEmpty()) {
                AsyncImage(model = ImageRequest.Builder(context).data(asset.thumbnailUrl)
                    .memoryCacheKey("${asset.target.baseUrl}:${asset.target.instanceId}:${asset.id}:${asset.version}:thumbnail")
                    .diskCacheKey("${asset.target.baseUrl}:${asset.target.instanceId}:${asset.id}:${asset.version}:thumbnail").build(), imageLoader = LocalMediaImages.current,
                    contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            }
            else Icon(Icons.Outlined.Image, "预览尚未生成", Modifier.align(Alignment.Center).size(28.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            if (asset.mediaType in setOf("video", "animated_image")) asset.durationMs?.takeIf { it > 0 }?.let { duration ->
                val seconds = duration / 1000
                Text(if (seconds >= 3600) "%d:%02d:%02d".format(seconds / 3600, seconds / 60 % 60, seconds % 60) else "%d:%02d".format(seconds / 60, seconds % 60),
                    Modifier.align(Alignment.BottomEnd).padding(8.dp), color = androidx.compose.ui.graphics.Color.White,
                    style = MaterialTheme.typography.labelSmall.copy(shadow = androidx.compose.ui.graphics.Shadow(blurRadius = 4f)))
            }
        }
        if (shortTitle) Text(asset.name.substringBeforeLast('.', asset.name), Modifier.fillMaxWidth().padding(horizontal = 5.dp), maxLines = 1,
            overflow = TextOverflow.Ellipsis, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
    }
}
