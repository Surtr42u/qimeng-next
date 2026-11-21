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
import androidx.compose.foundation.pager.PagerState
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
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shadow
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import coil3.compose.AsyncImage
import coil3.request.ImageRequest
import media.qimeng.next.AlbumBatchViewModel
import media.qimeng.next.AlbumSelectionState
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.ListState
import media.qimeng.next.model.Asset
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.model.Session
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

/** 首页顶部固定文案：提到文件级常量，滚动/翻页重组时不再重复分配列表。 */
private val HOME_TABS = listOf("推荐", "COS", "排行榜")

private val HOME_PERIODS = listOf("day" to "日", "week" to "周", "month" to "月", "year" to "年")

/** 网格 item 的 key/contentType 提到文件级：分页/加载重组时不再重建这两个 lambda。 */
private val ASSET_KEY: (Asset) -> Any = { it.id }

private val ASSET_CONTENT_TYPE: (Asset) -> Any? = { "asset" }

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
    val controls = remember(regular, cos, rank) { listOf(regular, cos, rank) }
    val capabilities by model.capabilities.collectAsStateWithLifecycle()
    // 固定回调只在依赖变化时重建，避免每次重组都让 HomeTopRow 失去跳过资格。
    val openSearch = remember(model) { { model.navigate("search") } }
    val openFilter = remember(cos) { { cos.showFilters.value = true } }
    // 翻页手势只经这一条 lambda 触发，胶囊栏重组不受影响。
    val selectPage: (Int) -> Unit = remember(scope, pager) { { page -> scope.launch { pager.scrollToPage(page) } } }
    Column(Modifier.fillMaxSize()) {
        HomeTopRow(controls, pager, homeBackdrop, openSearch, openFilter)
        HomeChips(period, pager, selectPage, onPeriodChange = { period = it })
        // 首页顶栏玻璃位于 pager 上方，其采样窗与 pager 区域不重叠（BackdropGlassPanel 只在本节点
        // 边界内采样），因此这层 backdrop 从未画出任何像素，只是每帧把整个网格再录一遍。
        // 去掉录制后像素不变，滚动时少了一次全网格离屏拷贝。
        HorizontalPager(pager, modifier = Modifier.weight(1f), key = { "home:$it" }) { page ->
            // targetPage 在 pager 内容里读取：翻页只重组已组合的页面，不再带上整个首页。
            val active = pager.targetPage
            val current = controls[active]
            if (capabilities == null) QimengSkeletonGrid(current.columns.intValue, current.columns.intValue * QIMENG_SKELETON_GRID_ROWS)
            else if ((page != 2 && capabilities?.recommendations == false) || (page == 2 && capabilities?.rankings == false)) {
                ContentCard(Modifier.padding(16.dp)) {
                    Text(if (page == 2) "排行榜服务尚未就绪" else "推荐服务尚未就绪")
                    OutlinedButton(onClick = model::refreshConnection) { Text("重试") }
                }
            } else {
                MediaBrowser(model, "home:$page", kind = if (page == 1) "cos" else "regular", bottomDock = true,
                    source = if (page == 2) "rankings" else "recommendations", seed = seeds.getOrElse(page) { 1L },
                    // 周期只作用于排行榜；此前三页共用 period，点一次周期会把两条推荐流也重查一遍
                    // （两次多余请求 + 列表重置）。
                    period = if (page == 2) period else "week",
                    onRefresh = if (page != 2) ({ seeds = seeds.mapIndexed { index, value -> if (index == page) kotlin.random.Random.nextLong(1, Long.MAX_VALUE) else value } }) else null,
                    controls = controls[page], toolbar = false, pinchEnabled = false, scrollTab = 0.takeIf { page == active })
            }
        }
    }
    // 常驻首页被其它 Tab / pushed 路由盖住时仍保持组合；筛选面板是 ModalBottomSheet
    // （独立窗口），若不随场景撤下会浮在当前路由之上。可见性由 ResidentSceneHost 提供，
    // 非常驻场景没有 provide，默认 true 保持原行为。
    if (LocalSceneActive.current) BrowserFilterSheet(cos)
}

/** 首页胶囊栏：只在这一条栏里读 targetPage，翻页时不再重组整页。 */
@Composable
private fun HomeChips(period: String, pager: PagerState, onSelectPage: (Int) -> Unit, onPeriodChange: (String) -> Unit) {
    val mode = pager.targetPage
    // 胶囊列表只在选中项变化时重建，无关重组不再让两条胶囊栏重新组合。
    val homePills = remember(mode) { HOME_TABS.mapIndexed { index, label -> QimengPill(label, mode == index) } }
    val periodPills = remember(period) { HOME_PERIODS.map { QimengPill(it.second, period == it.first) } }
    QimengChipRow(homePills, onPillClick = onSelectPage,
        pillModifier = Modifier.softDaylightShadow(QimengShapes.pill), modifier = Modifier.padding(horizontal = 16.dp))
    if (mode == 2) {
        Spacer(Modifier.height(4.dp))
        QimengChipRow(periodPills, { onPeriodChange(HOME_PERIODS[it].first) }, modifier = Modifier.padding(horizontal = 16.dp))
    }
}

@Composable
private fun HomeTopRow(
    controls: List<BrowserControls>,
    pager: PagerState,
    backdrop: QimengBackdropState?,
    onOpenSearch: () -> Unit,
    onOpenFilter: () -> Unit,
) {
    // 列数跟随当前页；targetPage 的读取收在顶栏内，翻页只重组这一行。
    val current = controls[pager.targetPage]
    val toggleColumns = remember(current, controls) {
        {
            val next = if (current.columns.intValue == 2) 1 else 2
            controls.forEach { it.columns.intValue = next }
        }
    }
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
                icon = if (current.columns.intValue == 1) Grid1Icon else gridIconFor(current.columns.intValue),
                contentDescription = "列数",
                onClick = toggleColumns,
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
                icon = if (current.columns.intValue == 1) Grid1Icon else gridIconFor(current.columns.intValue),
                contentDescription = "列数",
                onClick = toggleColumns,
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
    // 媒体库索引在会话建立时即加载（refreshConnection → loadLibraries）。无筛选相册的总数
    // 就是启用库 fileCount 之和（服务端 libraryProjection 与 GET /assets 默认谓词同为
    // trashed=0 + enabled），导航前已经拿到；用它给首帧计数兜底，列表响应随后覆盖。
    // 索引仍在加载或仍有下一页时为 null：宁可暂缺，也不拿残缺清单求和。
    val libraries by model.libraryPage.collectAsStateWithLifecycle()
    val seededTotal = remember(libraries) { model.libraryIndexTotal() }
    MediaBrowser(model, "albums", bottomDock = true, albumTitle = "相册", scrollTab = 1, totalSeed = seededTotal)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MediaBrowser(model: LibraryViewModel, key: String, favorite: Boolean? = null,
                 kind: String? = null, author: String? = null, bottomDock: Boolean = false,
                 source: String = "assets", seed: Long = 1, period: String = "week", onRefresh: (() -> Unit)? = null,
                 collectionId: String? = null, initialTag: String? = null, albumTitle: String? = null,
                 controls: BrowserControls = rememberBrowserControls(key, kind, initialTag.orEmpty()), toolbar: Boolean = true,
                 pinchEnabled: Boolean = true, textOverride: String? = null, caption: Boolean = false,
                 authorKind: String? = null, leading: (@Composable () -> Unit)? = null, scrollTab: Int? = null,
                 totalSeed: Long? = null) {
    // P4：组合期只做只读查询。缺失的列表由 effect 登记，避免在组合中创建/淘汰 ViewModel 的列表缓存
    // （淘汰会连带取消正在显示的列表的分页任务）。
    var listFlow by remember(key) { mutableStateOf(model.assets(key)) }
    LaunchedEffect(key) {
        val resolved = model.openAssets(key)
        if (listFlow !== resolved) listFlow = resolved
    }
    val listState = listFlow.collectAsStateWithLifecycle()
    val session by model.session.collectAsStateWithLifecycle()
    // 发布（上传入库）通知不在这里订阅：订阅会让每入库一个文件就重组一次列表，且过去把
    // publication 当 effect key，批量入库 100 个文件就整页重取 100 次。新鲜度由
    // LibraryViewModel.browse 自己比对 state.publicationId 与 UploadRepository.lastPublication，
    // 只在进入列表或回前台时检查一次（见下方 ON_RESUME）。
    val batch = if (key == "albums") viewModel<AlbumBatchViewModel>() else null
    // 选中集只在条目/选择栏内部读取；这里只派生窄状态，勾选一项不再重组浏览区与全部可见卡片。
    val selectionState: State<AlbumSelectionState?> =
        if (batch != null) batch.selection.collectAsStateWithLifecycle() else remember { mutableStateOf<AlbumSelectionState?>(null) }
    val selecting by remember(selectionState) { derivedStateOf { selectionState.value?.selecting == true } }
    val sheetOpen by remember(selectionState) { derivedStateOf { selectionState.value?.sheetOpen == true } }
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
    val query = controls.query(AssetQuery(text = textOverride.orEmpty(), kind = kind, favorite = favorite, authorId = author,
        sort = if (favorite == true) "favorited" else "default", source = source, seed = seed, period = period, collectionId = collectionId,
        history = true.takeIf { source == "history" })).let {
        if (favorite == true) it.copy(sort = "favorited", order = "desc") else it
    }
    // ListState 每页/每次加载都会换新对象：逐字段派生后，没变的读取不再让浏览区（及其可见卡片）重组。
    // 重查在途时 loadedQuery 仍是旧查询：只要列表已有内容（或曾经加载成功），就继续用现有条目与
    // 计数渲染，等响应落地再整页替换——筛选/分区/刷新期间不再清空网格或退回骨架。
    val queryLoaded by remember(listState, query) { derivedStateOf { listState.value.showsDataFor(query) } }
    val entries by remember(listState, query) {
        derivedStateOf {
            val value = listState.value
            if (value.showsDataFor(query)) value.items else emptyList()
        }
    }
    val listLoading by remember(listState) { derivedStateOf { listState.value.loading } }
    val listRefreshing by remember(listState) { derivedStateOf { listState.value.refreshing } }
    val listLoaded by remember(listState) { derivedStateOf { listState.value.loaded } }
    val listError by remember(listState) { derivedStateOf { listState.value.error } }
    val listCursor by remember(listState) { derivedStateOf { listState.value.cursor } }
    val totalMatched by remember(listState) { derivedStateOf { listState.value.totalMatched } }
    // 只有响应确实属于本次查询时，totalMatched 才能归到该查询名下；重查在途时它仍是上一个
    // 查询的数字，回写会让兜底计数串到另一组人群（见 LibraryViewModel.carriedTotals 的 KDoc）。
    val loadedTotal by remember(listState, query) {
        derivedStateOf { listState.value.totalMatched.takeIf { listState.value.loadedQuery == query } }
    }
    // 胶囊旁计数（相册/收藏/历史页头「N 文件」、作者作品页「筛选结果 N 个文件」）的兜底链：
    // 当前列表的权威计数 → 该 key 上一次成功计数（列表被淘汰/重建时仍保留）→ 导航前已拿到
    // 的种子（相册=启用库 fileCount 之和，作者页=点按行带入的 author.fileCount）。
    // 规则：显示过的数字不回退成空白；从未知道才允许缺省；不打印伪造的 0。
    // 只在查询仍是「无额外筛选」时使用种子——加了筛选就属于另一组人群，旧数字不能冒充。
    val carriedTotal = model.carriedTotal(key, query)
    LaunchedEffect(key, query, loadedTotal) { model.carryTotal(key, query, loadedTotal) }
    // 重查失败（queryLoaded=false）时旧查询的数字不再冒充新筛选的结果；同一查询的失败刷新
    // 仍因 loadedQuery == query 而保留计数，显示过的数字不回退成空白。
    val headerTotal = totalMatched.takeIf { queryLoaded } ?: carriedTotal
        ?: totalSeed?.takeIf { query.isUnfilteredCountScope(allowAuthor = author != null) }
    val dateCounts by remember(listState) { derivedStateOf { listState.value.dateCounts } }
    val unknownDateCount by remember(listState) { derivedStateOf { listState.value.unknownDateCount } }
    val group = if (albumTitle != null && leading == null) controls.row else null
    var appliedQuery by rememberSaveable(key) { mutableStateOf(query.toString() + group) }
    LaunchedEffect(key, session, query, group) {
        batch?.bindQuery(query)
        // 入库新鲜度不在这里触发：browse 自己比对 publicationId，进入列表时最多补一次。
        model.browse(key, query)
        val current = query.toString() + group
        if (appliedQuery != current) { grid.scrollToItem(0); appliedQuery = current }
    }
    // 回前台补一次新鲜度检查（browse 内部只在 publicationId 落后时才发请求，否则直接返回）。
    // 汇入 100 个文件只会让列表变「旧」，不会在这里变成 100 次请求：刷新发生在下一次
    // 进入列表或回前台，每次至多一次；手动下拉刷新不受影响。
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { model.browse(key, query) }
    val nearEnd by remember { derivedStateOf {
        val layout = grid.layoutInfo
        layout.totalItemsCount > 0 && (layout.visibleItemsInfo.lastOrNull()?.index ?: -1) >= layout.totalItemsCount - 8
    } }
    LaunchedEffect(nearEnd, listCursor, query) {
        if (nearEnd && listError == null) model.browse(key, query, next = true)
    }
    // 选择模式/面板开关只派生窄布尔值，勾选单项不会触发浏览区重组。
    if (batch != null) {
        BackHandler(enabled = selecting && !sheetOpen, onBack = batch::exit)
        if (sheetOpen) AlbumBatchSheetHost(batch, selectionState, model::refreshAfterAlbumBatch)
    }
    Column(Modifier.fillMaxSize()) {
        var rows: Map<String, media.qimeng.next.FacetRowState> = emptyMap()
        var filters: (@Composable () -> Unit)? = null
        if (albumTitle != null) {
            val facets: media.qimeng.next.FacetViewModel = androidx.lifecycle.viewmodel.compose.viewModel()
            val values by facets.rows.collectAsStateWithLifecycle()
            rows = values
            LaunchedEffect(session, query) { session?.let { facets.bind(it, query) } }
            filters = { AlbumFilters(albumTitle, headerTotal, controls, values,
                onBack = if (bottomDock) null else model::back, fetch = facets::fetch, authorKind = authorKind,
                loadFailed = listError != null, columns = columns, showTitle = !selecting,
                onToggleColumns = { setColumns(if (columns == 5) 2 else columns + 1) }) }
            if (batch != null && selecting) AlbumSelectionBarHost(batch, selectionState, entries)
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
            // 只有「当前查询确实加载过」才续页；重查失败后 loadedQuery 还是旧查询，
            // 用旧游标续新查询会拿错顺序，改为从第一页重取。
            model.browse(key, query, next = listCursor != null && listState.value.loadedQuery == query, force = true)
        }
        if (leading == null) listError?.let { ErrorNotice(it, retry) }
        val sectionMode = when {
            source == "history" -> "history"
            favorite == true -> "favorite"
            authorKind != null || caption -> "partitions"
            else -> group
        }
        val sections = remember(entries, sectionMode, rows, dateCounts, unknownDateCount) {
            gallerySections(entries, sectionMode, group?.let(rows::get)?.items.orEmpty(), dateCounts, unknownDateCount)
        }
        // 首页/搜索/收藏/历史与相册一样做视口前瞻预取（旧版 QimengMediaGrid 对全部网格生效）。
        // 带 leading 的网格下标含前导项，预取窗口会错位，故暂不接入。
        if (leading == null && entries.isNotEmpty()) PrefetchGalleryViewport(model, grid, sections, columns)
        PullToRefreshBox(isRefreshing = listRefreshing,
            onRefresh = {
                // 先让全库预取让路，刷新首屏与它在局域网带宽上不再互抢（旧版问题B 同款）。
                model.pauseThumbnailPrefetch()
                if (query.source == "recommendations" && onRefresh != null) onRefresh() else model.browse(key, query, force = true)
            },
            modifier = Modifier.weight(1f).testTag("media-browser:$key")) {
            // 骨架只覆盖「从未拿到数据」的列表：重查/返回详情时继续显示已有条目与计数。
            if (leading == null && entries.isEmpty() && !listLoaded && listError == null) {
                QimengSkeletonGrid(columns, columns * QIMENG_SKELETON_GRID_ROWS)
                return@PullToRefreshBox
            }
            if (leading == null && listLoaded && entries.isEmpty() && !listLoading && listError == null && key !in setOf("home:0", "home:1"))
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
                    item(key = "author-heading", contentType = "author-heading", span = { GridItemSpan(maxLineSpan) }) { leading() }
                    filters?.let { authorFilters ->
                        item(key = "author-filters", contentType = "author-filters", span = { GridItemSpan(maxLineSpan) }) { Column { authorFilters() } }
                    }
                    val message = listError
                    when {
                        message != null -> {
                            item(key = "author-error", contentType = "author-status", span = { GridItemSpan(maxLineSpan) }) {
                                Box(Modifier.padding(horizontal = QimengDimens.ScreenPaddingHorizontal, vertical = QimengDimens.SpaceM)) {
                                    ErrorNotice(message, retry)
                                }
                            }
                        }
                        !queryLoaded -> {
                            item(key = "author-loading", contentType = "author-status", span = { GridItemSpan(maxLineSpan) }) {
                                LinearProgressIndicator(Modifier.fillMaxWidth().padding(16.dp).testTag("author-media-loading"))
                            }
                        }
                        entries.isEmpty() && !listLoading -> {
                            item(key = "author-empty", contentType = "author-status", span = { GridItemSpan(maxLineSpan) }) {
                                Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) {
                                    Text("没有符合条件的媒体", style = MaterialTheme.typography.bodyMedium)
                                }
                            }
                        }
                    }
                }
                sections.forEach { section ->
                    section.header?.let { label -> item(key = "group:${section.key}", contentType = "group-header", span = { GridItemSpan(maxLineSpan) }) {
                        Text(label, Modifier.fillMaxWidth().padding(horizontal = 5.dp, vertical = 8.dp),
                            style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Bold)
                    } }
                    items(section.items, key = ASSET_KEY, contentType = ASSET_CONTENT_TYPE) { asset ->
                        // 选中集只在条目内读取（单次收集在浏览区外），勾选只失效本卡片。
                        val selectedState = remember(asset.id) { derivedStateOf { selectionState.value?.selected?.containsKey(asset.id) == true } }
                        val selected = if (selecting) selectedState.value else null
                        MediaTile(asset, caption, selected, onLongClick = if (batch != null && !selecting) ({ batch.select(asset) }) else null) {
                            if (selecting) batch?.select(asset) else model.openAsset(asset.id, key, asset.target, seed = asset)
                        }
                    }
                }
                if (key in setOf("home:0", "home:1") && queryLoaded && entries.isNotEmpty() && listCursor == null && listError == null)
                    item(key = "footer:end", contentType = "footer", span = { GridItemSpan(maxLineSpan) }) {
                        Text("已到底", Modifier.fillMaxWidth().padding(vertical = QimengDimens.SpaceL), textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
            }
        }
        if (batch != null && selecting) BatchActionButton(batch, selectionState)
    }
}

/** 选择栏自己读选中集：勾选只让这一条栏重组，不再带着浏览区整页重组。 */
@Composable
private fun AlbumSelectionBarHost(batch: AlbumBatchViewModel, selection: State<AlbumSelectionState?>, loaded: List<Asset>) {
    val state = selection.value ?: return
    AlbumSelectionBar(batch, state, loaded)
}

/** 批量面板同理：只有面板打开时才订阅完整选中集。 */
@Composable
private fun AlbumBatchSheetHost(batch: AlbumBatchViewModel, selection: State<AlbumSelectionState?>, onUpdated: (Session) -> Unit) {
    val state = selection.value ?: return
    AlbumBatchSheet(batch, state, onUpdated)
}

/** 底部批量按钮只在自己读已选数量：勾选一项只重组这条按钮，不再重组浏览区。 */
@Composable
private fun BatchActionButton(batch: AlbumBatchViewModel, selection: State<AlbumSelectionState?>) {
    val count = selection.value?.selected?.size ?: 0
    if (count <= 0) return
    Button(onClick = batch::openSheet, modifier = Modifier.fillMaxWidth().padding(
        start = 16.dp, end = 16.dp, bottom = TabDockDefaults.bottomClearance(), top = 4.dp,
    )) { Text("批量设置作者与来源 ($count)") }
}

/** 网格缩略图圆角沿用旧版 24px（像素密度换算，不是 dp）。 */
private const val TILE_CORNER_RADIUS_PX = 24f

/**
 * 这份列表状态是否有可以给 [query] 渲染的内容。重查在途时 `loadedQuery` 仍是上一个查询，
 * 列表条目会被保留：继续渲染它，网格与计数就不会在筛选/刷新期间闪空。请求失败后
 * （error != null）不再沿用上一个查询的条目与计数，否则新筛选会顶着旧结果冒充成功；
 * 同一查询的失败刷新仍由 `loadedQuery == query` 保住内容。
 */
private fun ListState<Asset>.showsDataFor(query: AssetQuery): Boolean =
    loadedQuery == query || (error == null && (loaded || items.isNotEmpty()))

/** 视频时长角标样式是常量，提到文件级避免逐项/逐次重组分配 TextStyle + Shadow。 */
private val TILE_DURATION_STYLE = TextStyle(
    fontSize = 12.sp,
    shadow = Shadow(Color.Black.copy(alpha = 0.6f), Offset(0f, 1f), 4f),
)

private val TILE_DURATION_COLOR = Color.White

@Composable
private fun MediaTile(asset: Asset, shortTitle: Boolean, isSelected: Boolean?, onLongClick: (() -> Unit)?, onClick: () -> Unit) {
    val context = LocalContext.current
    val density = LocalDensity.current
    // 形状按当前密度只算一次：原先每次重组都新建 Dp + RoundedCornerShape。
    val shape = remember(density) { RoundedCornerShape(with(density) { TILE_CORNER_RADIUS_PX.toDp() }) }
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
                // 同一资产的动图兜底请求只构建一次，避免每次重组都新建 ImageRequest。
                val animatedModel = remember(asset, context) {
                    ImageRequest.Builder(context).data(asset.contentUrl)
                        .memoryCacheKey("${asset.target.baseUrl}:${asset.target.instanceId}:${asset.id}:${asset.version}:animated-grid")
                        .diskCachePolicy(coil3.request.CachePolicy.DISABLED).build()
                }
                AsyncImage(model = animatedModel,
                    imageLoader = LocalMediaImages.current, contentDescription = null,
                    contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize(),
                    onError = { failed = true }, onSuccess = { failed = false })
            }
            else Icon(Icons.Outlined.Image, "预览尚未生成", Modifier.align(Alignment.Center).size(28.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            if (failed) Icon(Icons.Outlined.Image, "预览暂不可用", Modifier.align(Alignment.Center).size(28.dp),
                tint = MaterialTheme.colorScheme.onSurfaceVariant)
            if (isSelected == true) Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.primary.copy(alpha = 0.22f)))
            if (asset.mediaType == "video") detailDuration(asset.durationMs)?.let { badge ->
                Text(badge, Modifier.align(Alignment.BottomEnd).padding(8.dp), color = TILE_DURATION_COLOR, style = TILE_DURATION_STYLE)
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
