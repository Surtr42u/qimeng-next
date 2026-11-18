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
import media.qimeng.next.ui.TabScrollController
import media.qimeng.next.ui.component.QimengChipRow
import media.qimeng.next.ui.component.QimengPill
import media.qimeng.next.ui.component.QimengSkeletonGrid
import media.qimeng.next.ui.component.QIMENG_SKELETON_GRID_ROWS
import media.qimeng.next.ui.glass.*
import media.qimeng.next.ui.icon.*
import media.qimeng.next.ui.theme.QimengDimens
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
    val homeBackdrop = if (preferences.material.usesBackdrop) rememberQimengBackdropState(MaterialTheme.colorScheme.background) else null
    val regular = rememberBrowserControls("home:0", columns = 2)
    val cos = rememberBrowserControls("home:1", "cos", columns = 2)
    val rank = rememberBrowserControls("home:2", columns = 2)
    val controls = listOf(regular, cos, rank)
    val mode = pager.targetPage
    val current = controls[mode]
    val capabilities by model.capabilities.collectAsStateWithLifecycle()
    Column(Modifier.fillMaxSize()) {
        HomeTopRow(current.columns.intValue, homeBackdrop, { model.navigate("search") }, { cos.showFilters.value = true }) {
            val next = if (current.columns.intValue == 2) 1 else 2
            controls.forEach { it.columns.intValue = next }
        }
        QimengChipRow(listOf("推荐", "COS", "排行榜").mapIndexed { index, label -> QimengPill(label, mode == index) },
            onPillClick = { page -> scope.launch {
                pager.scrollToPage(page)
            } }, pillModifier = Modifier.softDaylightShadow(QimengShapes.pill), modifier = Modifier.padding(horizontal = 16.dp))
        if (mode == 2) {
            Spacer(Modifier.height(4.dp))
            val periods = listOf("day" to "日", "week" to "周", "month" to "月", "year" to "年")
            QimengChipRow(periods.map { QimengPill(it.second, period == it.first) }, { period = periods[it].first }, modifier = Modifier.padding(horizontal = 16.dp))
        }
        HorizontalPager(pager, modifier = Modifier.weight(1f).then(if (homeBackdrop != null) Modifier.qimengBackdropSource(homeBackdrop) else Modifier), key = { "home:$it" }) { page ->
            if (capabilities == null) QimengSkeletonGrid(current.columns.intValue, current.columns.intValue * QIMENG_SKELETON_GRID_ROWS)
            else if ((page != 2 && capabilities?.recommendations == false) || (page == 2 && capabilities?.rankings == false)) {
                ContentCard(Modifier.padding(16.dp)) {
                    Text(if (page == 2) "排行榜服务尚未就绪" else "推荐服务尚未就绪")
                    OutlinedButton(onClick = model::refreshConnection) { Text("重试") }
                }
            } else {
                MediaBrowser(model, "home:$page", kind = if (page == 1) "cos" else null, bottomDock = true,
                    source = if (page == 2) "rankings" else "recommendations", seed = seeds.getOrElse(page) { 1L }, period = period,
                    onRefresh = if (page != 2) ({ seeds = seeds.mapIndexed { index, value -> if (index == page) kotlin.random.Random.nextLong(1, Long.MAX_VALUE) else value } }) else null,
                    controls = controls[page], toolbar = false, pinchEnabled = false, scrollTab = 0.takeIf { page == mode })
            }
        }
    }
    BrowserFilterSheet(cos)
}

@Composable
private fun HomeTopRow(
    columns: Int,
    backdrop: QimengBackdropState?,
    onOpenSearch: () -> Unit,
    onOpenFilter: () -> Unit,
    onToggleColumns: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = "首页",
            style = MaterialTheme.typography.titleLarge.copy(
                fontSize = 24.sp,
                fontWeight = FontWeight.Bold,
                color = MaterialTheme.colorScheme.onSurface,
            ),
            modifier = Modifier.padding(end = 10.dp),
        )
        val searchInteraction = rememberPressScaleSource()
        if (backdrop != null) {
            BackdropGlassPanel(
                backdrop = backdrop,
                modifier = Modifier
                    .weight(1f)
                    .height(QimengDimens.HomeSearchFieldHeight)
                    .softDaylightShadow(QimengShapes.pill)
                    .pressScale(searchInteraction)
                    .clickable(
                        interactionSource = searchInteraction,
                        indication = null,
                        onClick = onOpenSearch,
                    ),
            ) {
                HomeSearchFieldContent()
            }
        } else {
            GlassSurface(
                shape = QimengShapes.pill,
                modifier = Modifier
                    .weight(1f)
                    .height(QimengDimens.HomeSearchFieldHeight)
                    .softDaylightShadow(QimengShapes.pill)
                    .pressScale(searchInteraction)
                    .clickable(
                        interactionSource = searchInteraction,
                        indication = null,
                        onClick = onOpenSearch,
                    ),
            ) {
                HomeSearchFieldContent()
            }
        }
        if (backdrop != null) {
            BackdropGlassIconButton(
                icon = HomeFilterIcon,
                contentDescription = "筛选",
                onClick = onOpenFilter,
                backdrop = backdrop,
                tint = MaterialTheme.colorScheme.primary,
                modifier = Modifier.padding(start = 10.dp, end = 6.dp).softDaylightShadow(QimengShapes.pill),
            )
            BackdropGlassIconButton(
                icon = if (columns == 1) Grid1Icon else gridIconFor(columns),
                contentDescription = "列数",
                onClick = onToggleColumns,
                modifier = Modifier.softDaylightShadow(QimengShapes.pill),
                backdrop = backdrop,
                tint = MaterialTheme.colorScheme.primary,
            )
        } else {
            GlassIconButton(
                icon = HomeFilterIcon,
                contentDescription = "筛选",
                onClick = onOpenFilter,
                tint = MaterialTheme.colorScheme.primary,
                modifier = Modifier.padding(start = 10.dp, end = 6.dp).softDaylightShadow(QimengShapes.pill),
            )
            GlassIconButton(
                icon = if (columns == 1) Grid1Icon else gridIconFor(columns),
                contentDescription = "列数",
                onClick = onToggleColumns,
                modifier = Modifier.softDaylightShadow(QimengShapes.pill),
                tint = MaterialTheme.colorScheme.primary,
            )
        }
    }
}

/** 搜索框文案内容（真采样/静态两个玻璃分支共用，避免内联重复） */
@Composable
private fun HomeSearchFieldContent() {
    Box(
        modifier = Modifier.fillMaxHeight(),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = "搜索",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(horizontal = 12.dp),
        )
    }
}

@Composable
fun AlbumsScreen(model: LibraryViewModel) {
    MediaBrowser(model, "albums", bottomDock = true, albumTitle = "相册", scrollTab = 1)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MediaBrowser(model: LibraryViewModel, key: String, favorite: Boolean? = null,
                 kind: String? = null, author: String? = null, bottomDock: Boolean = false,
                 source: String = "assets", seed: Long = 1, period: String = "week", onRefresh: (() -> Unit)? = null,
                 collectionId: String? = null, initialTag: String? = null, albumTitle: String? = null,
                 controls: BrowserControls = rememberBrowserControls(key, kind, initialTag.orEmpty()), toolbar: Boolean = true,
                 pinchEnabled: Boolean = true, textOverride: String? = null, caption: Boolean = false,
                 authorKind: String? = null, leading: (@Composable () -> Unit)? = null, scrollTab: Int? = null) {
    val state by model.assets(key).collectAsStateWithLifecycle()
    val session by model.session.collectAsStateWithLifecycle()
    var columns by controls.columns
    val grid = rememberLazyGridState()
    LaunchedEffect(scrollTab, grid) { if (scrollTab != null) TabScrollController.events.collect { if (it == scrollTab) grid.scrollToItem(0) } }
    val scope = rememberCoroutineScope()
    val query = controls.query(AssetQuery(text = textOverride.orEmpty(), kind = kind, favorite = favorite, authorId = author,
        sort = if (favorite == true) "favorited" else "default", source = source, seed = seed, period = period, collectionId = collectionId,
        history = true.takeIf { source == "history" }))
    val group = if (albumTitle != null) controls.row else null
    var appliedQuery by rememberSaveable(key) { mutableStateOf(query.toString() + group) }
    LaunchedEffect(key, query, group) {
        model.browse(key, query)
        val current = query.toString() + group
        if (appliedQuery != current) { grid.scrollToItem(0); appliedQuery = current }
    }
    val nearEnd by remember { derivedStateOf {
        val layout = grid.layoutInfo
        layout.totalItemsCount > 0 && (layout.visibleItemsInfo.lastOrNull()?.index ?: -1) >= layout.totalItemsCount - 8
    } }
    LaunchedEffect(nearEnd, state.cursor, query) {
        if (nearEnd && state.error == null) model.browse(key, query, next = true)
    }
    Column(Modifier.fillMaxSize()) {
        var rows: Map<String, media.qimeng.next.FacetRowState> = emptyMap()
        var filters: (@Composable () -> Unit)? = null
        if (albumTitle != null) {
            val facets: media.qimeng.next.FacetViewModel = androidx.lifecycle.viewmodel.compose.viewModel()
            val values by facets.rows.collectAsStateWithLifecycle()
            rows = values
            LaunchedEffect(session, query) { session?.let { facets.bind(it, query) } }
            filters = { AlbumFilters(albumTitle, state.totalMatched.takeIf { state.loadedQuery == query }, controls, values,
                onBack = if (bottomDock) null else model::back, fetch = facets::fetch, authorKind = authorKind) }
            if (leading == null) filters?.invoke()
        } else if (toolbar) Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp)) {
            listOf(null to "综合", "video" to "视频", "animated_image" to "动图", "image" to "图片").forEach { (value, label) ->
                FilterChip(controls.type.value == value, { controls.type.value = value }, label = { Text(label) })
            }
            IconButton(onClick = { controls.showFilters.value = true }) { Icon(Icons.Outlined.Tune, "筛选") }
            IconButton(onClick = { columns = if (columns == 5) 2 else columns + 1 }) { Icon(Icons.Outlined.GridView, "列数") }
        }
        if (albumTitle != null || toolbar) BrowserFilterSheet(controls)
        state.error?.let { ErrorNotice(it) {
            if (state.reloadFromStart) {
                if (onRefresh != null) onRefresh() else {
                    scope.launch { grid.scrollToItem(0) }; model.browse(key, query, force = true)
                }
            } else model.browse(key, query, next = state.cursor != null && state.loadedQuery == query, force = true)
        } }
        val entries = if (state.loadedQuery == query) state.items else emptyList()
        val sectionMode = when { source == "history" -> "history"; authorKind != null || favorite == true || caption -> "partitions"; else -> group }
        val sections = remember(entries, sectionMode, rows, state.dateCounts, state.unknownDateCount) {
            gallerySections(entries, sectionMode, group?.let(rows::get)?.items.orEmpty(), state.dateCounts, state.unknownDateCount)
        }
        PullToRefreshBox(isRefreshing = state.loading && state.loaded && state.cursor == null,
            onRefresh = { if (query.source == "recommendations" && onRefresh != null) onRefresh() else model.browse(key, query, force = true) },
            modifier = Modifier.weight(1f).testTag("media-browser:$key")) {
            if (!state.loaded && state.error == null) {
                QimengSkeletonGrid(columns, columns * QIMENG_SKELETON_GRID_ROWS)
                return@PullToRefreshBox
            }
            if (state.loaded && entries.isEmpty() && !state.loading && key !in setOf("home:0", "home:1"))
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { Text(if (key == "home:2") "暂无数据" else "没有符合条件的媒体") }
            LazyVerticalGrid(columns = GridCells.Fixed(columns), state = grid,
                contentPadding = PaddingValues(start = QimengDimens.SpaceM, end = QimengDimens.SpaceM, bottom = if (bottomDock) TabDockDefaults.bottomClearance() else 24.dp),
                modifier = Modifier.padding(top = QimengDimens.SpaceM).fillMaxSize().pointerInput(key, pinchEnabled) {
                    if (!pinchEnabled) return@pointerInput
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
                if (leading != null) item(key = "author-heading", span = { GridItemSpan(maxLineSpan) }) { leading(); filters?.invoke() }
                sections.forEach { section ->
                    section.header?.let { label -> item(key = "group:${section.key}", span = { GridItemSpan(maxLineSpan) }) {
                        Text(label, Modifier.fillMaxWidth().padding(horizontal = 5.dp, vertical = 8.dp),
                            style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Bold)
                    } }
                    items(section.items, key = Asset::id) { asset -> MediaTile(asset, caption) { model.openAsset(asset.id, key) } }
                }
                if (key in setOf("home:0", "home:1") && state.loadedQuery == query && entries.isNotEmpty() && state.cursor == null && state.error == null)
                    item(key = "footer:end", span = { GridItemSpan(maxLineSpan) }) {
                        Text("已到底", Modifier.fillMaxWidth().padding(vertical = QimengDimens.SpaceL), textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
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
    val animated = asset.mediaType == "animated_image"
    val source = if (animated) asset.contentUrl else asset.thumbnailUrl
    val cacheKey = "${asset.target.baseUrl}:${asset.target.instanceId}:${asset.id}:${asset.version}:" + if (animated) "animated-grid" else "thumbnail"
    Column {
        Box(Modifier.padding(5.dp).fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(radius))
            .background(MaterialTheme.colorScheme.secondaryContainer).clickable(onClick = onClick).semantics { contentDescription = asset.name }) {
            if (source.isNotEmpty()) {
                AsyncImage(model = ImageRequest.Builder(context).data(source)
                    .memoryCacheKey(cacheKey).diskCacheKey(cacheKey)
                    .diskCachePolicy(if (animated) coil3.request.CachePolicy.DISABLED else coil3.request.CachePolicy.ENABLED).build(), imageLoader = LocalMediaImages.current,
                    contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            }
            else Icon(Icons.Outlined.Image, "预览尚未生成", Modifier.align(Alignment.Center).size(28.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            if (asset.mediaType == "video") detailDuration(asset.durationMs)?.let { badge ->
                Text(badge,
                    Modifier.align(Alignment.BottomEnd).padding(8.dp), color = androidx.compose.ui.graphics.Color.White,
                    style = androidx.compose.ui.text.TextStyle(fontSize = 12.sp, shadow = androidx.compose.ui.graphics.Shadow(
                        androidx.compose.ui.graphics.Color.Black.copy(alpha = 0.6f), androidx.compose.ui.geometry.Offset(0f, 1f), 4f)))
            }
        }
        if (shortTitle) Text(asset.name.substringBeforeLast('.', asset.name), Modifier.fillMaxWidth().padding(horizontal = 5.dp), maxLines = 1,
            overflow = TextOverflow.Ellipsis, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
    }
}
