package media.qimeng.next.screen

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.grid.*
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.shape.CircleShape
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import coil3.compose.AsyncImage
import coil3.request.ImageRequest
import media.qimeng.next.AlbumBatchViewModel
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
                MediaBrowser(model, "home:$page", kind = if (page == 1) "cos" else "regular", bottomDock = true,
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
    val publication by model.lastPublication.collectAsStateWithLifecycle()
    val batch = if (key == "albums") viewModel<AlbumBatchViewModel>() else null
    val selection = batch?.selection?.collectAsStateWithLifecycle()?.value
    val selecting = selection?.selecting == true
    val sharedColumns = key in setOf("albums", "favorites", "history")
    val committedColumns = if (sharedColumns) model.albumColumns.collectAsStateWithLifecycle().value else controls.columns.intValue
    var pinchColumns by remember(key) { mutableStateOf<Int?>(null) }
    val columns = pinchColumns ?: committedColumns
    val currentColumns by rememberUpdatedState(committedColumns)
    val setColumns by rememberUpdatedState<(Int) -> Unit> { count ->
        if (sharedColumns) model.saveAlbumColumns(count) else controls.columns.intValue = count
    }
    val grid = rememberLazyGridState()
    LaunchedEffect(scrollTab, grid) { if (scrollTab != null) TabScrollController.events.collect { if (it == scrollTab) grid.scrollToItem(0) } }
    val scope = rememberCoroutineScope()
    val query = controls.query(AssetQuery(text = textOverride.orEmpty(), kind = kind, favorite = favorite, authorId = author,
        sort = if (favorite == true) "favorited" else "default", source = source, seed = seed, period = period, collectionId = collectionId,
        history = true.takeIf { source == "history" })).let {
        if (favorite == true) it.copy(sort = "favorited", order = "desc") else it
    }
    val group = if (albumTitle != null && leading == null) controls.row else null
    var appliedQuery by rememberSaveable(key) { mutableStateOf(query.toString() + group) }
    LaunchedEffect(key, session, query, group, publication) {
        batch?.bindQuery(query)
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
    val entries = if (state.loadedQuery == query) state.items else emptyList()
    if (batch != null && selection != null) {
        BackHandler(enabled = selecting && !selection.sheetOpen, onBack = batch::exit)
        if (selection.sheetOpen) AlbumBatchSheet(batch, selection, model::refreshAfterAlbumBatch)
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
                onBack = if (bottomDock) null else model::back, fetch = facets::fetch, authorKind = authorKind,
                loadFailed = state.error != null, columns = columns, showTitle = !selecting,
                onToggleColumns = { setColumns(if (columns == 5) 2 else columns + 1) }) }
            if (batch != null && selection != null && selecting) AlbumSelectionBar(batch, selection, entries)
            if (leading == null) filters?.invoke()
        } else if (toolbar) Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp)) {
            listOf(null to "综合", "video" to "视频", "animated_image" to "动图", "image" to "图片").forEach { (value, label) ->
                FilterChip(controls.type.value == value, { controls.type.value = value }, label = { Text(label) })
            }
            IconButton(onClick = { controls.showFilters.value = true }) { Icon(Icons.Outlined.Tune, "筛选") }
            IconButton(onClick = { setColumns(if (columns == 5) 2 else columns + 1) }) { Icon(Icons.Outlined.GridView, "列数") }
        }
        if (albumTitle != null || toolbar) BrowserFilterSheet(controls)
        val retry: () -> Unit = {
            if (state.reloadFromStart) {
                if (onRefresh != null) onRefresh() else {
                    scope.launch { grid.scrollToItem(0) }; model.browse(key, query, force = true)
                }
            } else model.browse(key, query, next = state.cursor != null && state.loadedQuery == query, force = true)
        }
        if (leading == null) state.error?.let { ErrorNotice(it, retry) }
        val sectionMode = when {
            source == "history" -> "history"
            favorite == true -> "favorite"
            authorKind != null || caption -> "partitions"
            else -> group
        }
        val sections = remember(entries, sectionMode, rows, state.dateCounts, state.unknownDateCount) {
            gallerySections(entries, sectionMode, group?.let(rows::get)?.items.orEmpty(), state.dateCounts, state.unknownDateCount)
        }
        if (sharedColumns && entries.isNotEmpty()) PrefetchGalleryViewport(model, grid, sections, columns)
        PullToRefreshBox(isRefreshing = state.loading && state.loaded && state.cursor == null,
            onRefresh = { if (query.source == "recommendations" && onRefresh != null) onRefresh() else model.browse(key, query, force = true) },
            modifier = Modifier.weight(1f).testTag("media-browser:$key")) {
            if (leading == null && !state.loaded && state.error == null) {
                QimengSkeletonGrid(columns, columns * QIMENG_SKELETON_GRID_ROWS)
                return@PullToRefreshBox
            }
            if (leading == null && state.loaded && entries.isEmpty() && !state.loading && key !in setOf("home:0", "home:1"))
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { Text(if (key == "home:2") "暂无数据" else "没有符合条件的媒体") }
            LazyVerticalGrid(columns = GridCells.Fixed(columns), state = grid,
                contentPadding = PaddingValues(start = QimengDimens.SpaceM, end = QimengDimens.SpaceM, bottom = when {
                    bottomDock && !selecting -> TabDockDefaults.bottomClearance()
                    leading != null -> QimengDimens.ListBottomContentPadding
                    else -> 24.dp
                }),
                modifier = Modifier.padding(top = QimengDimens.SpaceM).fillMaxSize()
                    .then(if (sharedColumns) Modifier.testTag("gallery-grid:$key").semantics { stateDescription = "$columns 列" } else Modifier)
                    .then(if (leading == null) Modifier else Modifier.testTag("author-media-scroll")).pointerInput(key, pinchEnabled) {
                    if (!pinchEnabled) return@pointerInput
                    awaitEachGesture {
                        awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
                        val initialColumns = currentColumns
                        var pendingColumns = initialColumns
                        var accumulated = 1f
                        try {
                            do {
                                val event = awaitPointerEvent(PointerEventPass.Initial)
                                if (event.changes.count { it.pressed } >= 2) {
                                    accumulated *= event.calculateZoom()
                                    val step = when {
                                        accumulated > 1.22f -> -1
                                        accumulated < 0.82f -> 1
                                        else -> 0
                                    }
                                    if (step != 0) {
                                        pendingColumns = (pendingColumns + step).coerceIn(2, 5)
                                        pinchColumns = pendingColumns
                                        accumulated = 1f
                                    }
                                    event.changes.forEach { it.consume() }
                                }
                            } while (event.changes.any { it.pressed })
                            if (pendingColumns != initialColumns) setColumns(pendingColumns)
                        } finally {
                            pinchColumns = null
                        }
                    }
                }) {
                if (leading != null) {
                    item(key = "author-heading", span = { GridItemSpan(maxLineSpan) }) { leading() }
                    filters?.let { authorFilters ->
                        item(key = "author-filters", span = { GridItemSpan(maxLineSpan) }) { Column { authorFilters() } }
                    }
                    val message = state.error
                    when {
                        message != null -> {
                            item(key = "author-error", span = { GridItemSpan(maxLineSpan) }) {
                                Box(Modifier.padding(horizontal = QimengDimens.ScreenPaddingHorizontal, vertical = QimengDimens.SpaceM)) {
                                    ErrorNotice(message, retry)
                                }
                            }
                        }
                        state.loadedQuery != query -> {
                            item(key = "author-loading", span = { GridItemSpan(maxLineSpan) }) {
                                LinearProgressIndicator(Modifier.fillMaxWidth().padding(16.dp).testTag("author-media-loading"))
                            }
                        }
                        entries.isEmpty() && !state.loading -> {
                            item(key = "author-empty", span = { GridItemSpan(maxLineSpan) }) {
                                Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) {
                                    Text("没有符合条件的媒体", style = MaterialTheme.typography.bodyMedium)
                                }
                            }
                        }
                    }
                }
                sections.forEach { section ->
                    section.header?.let { label -> item(key = "group:${section.key}", span = { GridItemSpan(maxLineSpan) }) {
                        Text(label, Modifier.fillMaxWidth().padding(horizontal = 5.dp, vertical = 8.dp),
                            style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Bold)
                    } }
                    items(section.items, key = Asset::id) { asset ->
                        val selected = if (selecting) selection?.selected?.containsKey(asset.id) else null
                        MediaTile(asset, caption, selected, onLongClick = if (batch != null && !selecting) ({ batch.select(asset) }) else null) {
                            if (selecting) batch?.select(asset) else model.openAsset(asset.id, key, asset.target, seed = asset)
                        }
                    }
                }
                if (key in setOf("home:0", "home:1") && state.loadedQuery == query && entries.isNotEmpty() && state.cursor == null && state.error == null)
                    item(key = "footer:end", span = { GridItemSpan(maxLineSpan) }) {
                        Text("已到底", Modifier.fillMaxWidth().padding(vertical = QimengDimens.SpaceL), textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
            }
        }
        if (batch != null && selection != null && selecting && selection.selected.isNotEmpty()) {
            Button(onClick = batch::openSheet, modifier = Modifier.fillMaxWidth().padding(
                start = 16.dp, end = 16.dp, bottom = TabDockDefaults.bottomClearance(), top = 4.dp,
            )) { Text("批量设置作者与来源 (${selection.selected.size})") }
        }
    }
}

@Composable
private fun MediaTile(asset: Asset, shortTitle: Boolean, isSelected: Boolean?, onLongClick: (() -> Unit)?, onClick: () -> Unit) {
    val context = LocalContext.current
    val radius = with(LocalDensity.current) { 24f.toDp() }
    // Existing AssetCard: 16:9 frame, 5dp outer spacing and a 24px corner; only search adds a caption.
    val animated = asset.mediaType == "animated_image"
    var failed by remember(asset.id, asset.version) { mutableStateOf(false) }
    var attempt by remember(asset.id, asset.version) { mutableIntStateOf(0) }
    LaunchedEffect(asset.id, failed, attempt) {
        // 个别预览在服务端刚生成或刚换缓存池时会失败一次；给两次短暂重试，失败再显示占位图标。
        if (failed && attempt < 2) { kotlinx.coroutines.delay(1_200L); attempt++; failed = false }
    }
    val thumbModel = remember(asset, attempt) {
        if (attempt == 0) asset else ImageRequest.Builder(context).data(asset)
            .memoryCacheKey("${asset.target.baseUrl}:${asset.target.instanceId}:${asset.id}:${asset.version}:grid-retry-$attempt")
            .build()
    }
    val shape = RoundedCornerShape(radius)
    val click = if (onLongClick != null) Modifier.combinedClickable(onClick = onClick, onLongClick = onLongClick, onLongClickLabel = "选择媒体")
        else Modifier.clickable(onClick = onClick)
    Column {
        Box(Modifier.padding(5.dp).fillMaxWidth().aspectRatio(16f / 9f).clip(shape)
            .then(if (isSelected == true) Modifier.border(2.5.dp, MaterialTheme.colorScheme.primary, shape) else Modifier)
            .background(MaterialTheme.colorScheme.secondaryContainer).then(click).semantics {
                contentDescription = asset.name
                if (isSelected != null) selected = isSelected
            }) {
            if (asset.thumbnailUrl.isNotEmpty()) {
                // 网格优先用已缓存的 512 预览，动图也不再拉整份原件；否则大动图会长时间白框。
                AsyncImage(model = thumbModel, imageLoader = LocalMediaImages.current,
                    contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize(),
                    onError = { failed = true }, onSuccess = { failed = false })
            } else if (animated) {
                AsyncImage(model = ImageRequest.Builder(context).data(asset.contentUrl)
                    .memoryCacheKey("${asset.target.baseUrl}:${asset.target.instanceId}:${asset.id}:${asset.version}:animated-grid")
                    .diskCachePolicy(coil3.request.CachePolicy.DISABLED).build(),
                    imageLoader = LocalMediaImages.current, contentDescription = null,
                    contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize(),
                    onError = { failed = true }, onSuccess = { failed = false })
            }
            else Icon(Icons.Outlined.Image, "预览尚未生成", Modifier.align(Alignment.Center).size(28.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            if (failed) Icon(Icons.Outlined.Image, "预览暂不可用", Modifier.align(Alignment.Center).size(28.dp),
                tint = MaterialTheme.colorScheme.onSurfaceVariant)
            if (isSelected == true) Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.primary.copy(alpha = 0.22f)))
            if (asset.mediaType == "video") detailDuration(asset.durationMs)?.let { badge ->
                Text(badge,
                    Modifier.align(Alignment.BottomEnd).padding(8.dp), color = androidx.compose.ui.graphics.Color.White,
                    style = androidx.compose.ui.text.TextStyle(fontSize = 12.sp, shadow = androidx.compose.ui.graphics.Shadow(
                        androidx.compose.ui.graphics.Color.Black.copy(alpha = 0.6f), androidx.compose.ui.geometry.Offset(0f, 1f), 4f)))
            }
            if (isSelected != null) {
                val marker = Modifier.align(Alignment.TopEnd).padding(8.dp).size(24.dp)
                if (isSelected) {
                    Box(marker.background(MaterialTheme.colorScheme.primary, CircleShape), contentAlignment = Alignment.Center) {
                        Icon(CheckMarkIcon, null, Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onPrimary)
                    }
                } else {
                    Box(marker.background(Color.Black.copy(alpha = 0.35f), CircleShape)
                        .border(1.5.dp, Color.White.copy(alpha = 0.85f), CircleShape))
                }
            }
        }
        if (shortTitle) Text(asset.name.substringBeforeLast('.', asset.name), Modifier.fillMaxWidth().padding(horizontal = 5.dp), maxLines = 1,
            overflow = TextOverflow.Ellipsis, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
    }
}
