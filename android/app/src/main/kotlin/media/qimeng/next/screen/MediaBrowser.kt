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
import media.qimeng.next.LIST_UP_TO_DATE_MESSAGE
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.ListState
import media.qimeng.next.sameEndpoint
import media.qimeng.next.shouldOfferLocalIndexRebuild
import media.qimeng.next.shouldOfferLocalRecovery
import media.qimeng.next.shouldPreferRuntimeFailureMessage
import media.qimeng.next.shouldRestartLocalRuntime
import media.qimeng.next.model.Asset
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.model.Session
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNoticeAction
import media.qimeng.next.ui.ErrorNoticeWithActions
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
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

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
    // 首页推荐/COS 两格共用一份持久化列数（冷启动恢复上次档位）；排行页没有独立偏好，
    // 以同一档位起步，顶栏切换仍会同时作用于三页。
    val homeColumns by model.homeColumns.collectAsStateWithLifecycle()
    val regular = rememberBrowserControls("home:0", columns = homeColumns, minColumns = HOME_MIN_GRID_COLUMNS)
    val cos = rememberBrowserControls("home:1", "cos", columns = homeColumns, minColumns = HOME_MIN_GRID_COLUMNS)
    val rank = rememberBrowserControls("home:2", columns = homeColumns, minColumns = HOME_MIN_GRID_COLUMNS)
    val controls = remember(regular, cos, rank) { listOf(regular, cos, rank) }
    // 三页的网格状态提到 pager 之外：pager 只为已组合的页面保留可保存状态（index+offset），
    // 页面被回收后失去 LazyGrid 的 key 锚点，返回时只能按条目下标还原。常驻首页在整段
    // 运行期都持有这三份状态，翻页/离开再回来都不重建。
    val regularGrid = rememberLazyGridState()
    val cosGrid = rememberLazyGridState()
    val rankGrid = rememberLazyGridState()
    val homeGrids = remember(regularGrid, cosGrid, rankGrid) { listOf(regularGrid, cosGrid, rankGrid) }
    val sceneActive = LocalSceneActive.current
    // 常驻 Tab 被其它 Tab / pushed 路由盖住时仍保持组合；筛选面板是 ModalBottomSheet
    // （独立窗口），触摸死层盖不到它，若不随场景撤下会浮在当前路由之上。showFilters 同时
    // 复位：面板若只是被移除而标志仍为 true，回到该 Tab 时 if (sceneActive) 一恢复就会自动重开。
    LaunchedEffect(sceneActive) { if (!sceneActive) controls.forEach { it.showFilters.value = false } }
    val saveHomeColumns = remember(model) { model::saveHomeColumns }
    val capabilities by model.capabilities.collectAsStateWithLifecycle()
    // 固定回调只在依赖变化时重建，避免每次重组都让 HomeTopRow 失去跳过资格。
    val openSearch = remember(model) { { model.navigate("search") } }
    val openFilter = remember(cos) { { cos.showFilters.value = true } }
    // 翻页手势只经这一条 lambda 触发，胶囊栏重组不受影响。
    val selectPage: (Int) -> Unit = remember(scope, pager) { { page -> scope.launch { pager.scrollToPage(page) } } }
    Column(Modifier.fillMaxSize()) {
        HomeTopRow(controls, pager, homeBackdrop, openSearch, openFilter, saveHomeColumns)
        HomeChips(period, pager, selectPage, onPeriodChange = { period = it })
        // 首页顶栏玻璃位于 pager 上方，其采样窗与 pager 区域不重叠（BackdropGlassPanel 只在本节点
        // 边界内采样），因此这层 backdrop 从未画出任何像素，只是每帧把整个网格再录一遍。
        // 去掉录制后像素不变，滚动时少了一次全网格离屏拷贝。
        // beyondViewportPageCount=1：只预组合当前页相邻的一页（另一侧在组合窗口之外，不占
        // 内存），滑动时邻页已有首屏内容而不是骨架。邻页以 prefetchEnabled=false 组合：不起
        // 视口前瞻 worker、不续页，但它自身首屏可见格子的 AsyncImage 仍会随组合取缩略图——
        // 这是「滑过去即显」的代价（审查 P2 已记档），不是零下载。
        HorizontalPager(pager, modifier = Modifier.weight(1f), key = { "home:$it" }, beyondViewportPageCount = 1) { page ->
            // targetPage 在 pager 内容里读取：翻页只重组已组合的页面，不再带上整个首页。
            val active = pager.targetPage
            // 首帧不等 /instance：能力位只决定"服务未就绪"提示，不决定是否组合网格。
            // 旧版同样是先组合网格、再等就绪探测；这样冷启动少一个整往返，骨架只随列表加载出现。
            if ((page != 2 && capabilities?.recommendations == false) || (page == 2 && capabilities?.rankings == false)) {
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
                    controls = controls[page], toolbar = false, pinchEnabled = false, scrollTab = 0.takeIf { page == active },
                    gridState = homeGrids[page],
                    // 只有当前页（targetPage）起视口预取；预组合的邻页仍登记列表并取第一页，
                    // 但不另开预取 worker/缩略图下载，避免与正在看的页面抢宽带。
                    prefetchEnabled = page == active)
            }
        }
    }
    // 可见性由 ResidentSceneHost 提供（见上：失活时连同 showFilters 一并复位），
    // 非常驻场景没有 provide，默认 true 保持原行为。
    if (sceneActive) BrowserFilterSheet(cos)
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
    onColumnsCommitted: (Int) -> Unit,
) {
    // 列数跟随当前页；targetPage 的读取收在顶栏内，翻页只重组这一行。
    val current = controls[pager.targetPage]
    val toggleColumns = remember(current, controls, onColumnsCommitted) {
        {
            // 首页顶栏是 1 列 ↔ 2 列切换（单列大图流，图标 Grid1Icon 同源）；网格捏合仍只管 2~5 档。
            // 持久化范围已包含 1，重启后原样恢复，不会被截成两列。
            val next = if (current.columns.intValue == MIN_GRID_COLUMNS) HOME_MIN_GRID_COLUMNS else MIN_GRID_COLUMNS
            controls.forEach { it.columns.intValue = next }
            onColumnsCommitted(next)
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
                 totalSeed: Long? = null, gridState: LazyGridState = rememberLazyGridState(),
                 // 首页 pager 的预组合邻页传 false：照常登记列表、取第一页数据，但不启动
                 // 自己的视口前瞻预取。默认 true 保持搜索/收藏/历史/相册等既有行为。
                 prefetchEnabled: Boolean = true) {
    // P4：组合期只做只读查询。缺失的列表由 effect 登记，避免在组合中创建/淘汰 ViewModel 的列表缓存
    // （淘汰会连带取消正在显示的列表的分页任务）。
    var listFlow by remember(key) { mutableStateOf(model.assets(key)) }
    LaunchedEffect(key) {
        val resolved = model.openAssets(key)
        if (listFlow !== resolved) listFlow = resolved
    }
    val listState = listFlow.collectAsStateWithLifecycle()
    val session by model.session.collectAsStateWithLifecycle()
    // 本机运行层的回环入口（远程包为空串，判定先看 localRuntimeAvailable）。
    val localRuntimeAddress by model.localAddress.collectAsStateWithLifecycle()
    val localRuntimeStatus by model.localRuntimeStatus.collectAsStateWithLifecycle()
    // 归属可能来自启动回执确认过的旧端口（ownedEndpoints），也可能是当前配置的 baseUrl；
    // 两者都算本机端点，不能用 IP 字面量判定。
    val currentIsLocal = model.isLocalEndpoint(session) || sameEndpoint(session?.baseUrl, localRuntimeAddress)
    val preferRuntimeMessage = shouldPreferRuntimeFailureMessage(
        model.localRuntimeAvailable, currentIsLocal, localRuntimeStatus.phase, localRuntimeStatus.message)
    val localRuntimeNotice = localRuntimeStatus.message.takeIf { preferRuntimeMessage }
    // 子进程带退出码退出说明控制目录里的索引数据让服务启不起来：重启运行层不会自愈，
    // 横幅补充需要确认的「重建本机索引」。只在用户点按后才执行，绝不自动重建。
    val offerIndexRebuild = shouldOfferLocalIndexRebuild(
        model.localRuntimeAvailable, currentIsLocal, localRuntimeStatus.phase, localRuntimeStatus.message)
    var confirmIndexRebuild by remember(key) { mutableStateOf(false) }
    // 主页面不渲染 AppRoot 的全局提示条（那里只覆盖 pushed 路由）；刷新「内容没有变化」
    // 的轻提示由本组件在主页面内落地，避免消息设了却看不见。
    val route by model.route.collectAsStateWithLifecycle()
    val browserMessage by model.message.collectAsStateWithLifecycle()
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
    val grid = gridState
    val sceneActive = LocalSceneActive.current
    // 常驻 Tab 隐藏时仍保持组合：alpha=0 与触摸死层只能挡住新手势，挡不住已经在跑的 fling，
    // 网格会在被盖住时继续滑。场景从可见翻到隐藏的那一刻把首项原地扶正（index + offset），
    // 等效冻结滚动。LocalSceneActive 对所有常驻 Tab（tab:0..3）提供 false，pushed 路由
    // 没有 provide、默认 true，因此非常驻页面不会走这里。
    var sceneWasActive by remember(key) { mutableStateOf(sceneActive) }
    LaunchedEffect(sceneActive, grid) {
        val justHidden = sceneWasActive && !sceneActive
        sceneWasActive = sceneActive
        if (justHidden) grid.scrollToItem(grid.firstVisibleItemIndex, grid.firstVisibleItemScrollOffset)
    }
    // 常驻 Tab 失活时撤下本浏览区的独立窗口：筛选面板与相册批量面板都是 ModalBottomSheet，
    // 属于单独窗口，触摸死层盖不住。showFilters 必须一起复位，否则回到该 Tab 时会立即重开；
    // 批量面板走 AlbumBatchViewModel.closeSheet()：只清 sheetOpen（写入在途时保留进度/报告），
    // 不清已选集。
    LaunchedEffect(sceneActive) {
        if (!sceneActive) {
            controls.showFilters.value = false
            batch?.closeSheet()
        }
    }
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
    // 本查询的首屏结果是否已经落地：重查在途时 loadedQuery 仍是上一个查询，旧条目只是
    // 暂留显示。下拉刷新归位要求列表确实属于当前查询，避免在旧数据上提前归位。
    val querySettled by remember(listState, query) { derivedStateOf { listState.value.loadedQuery == query } }
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
    // 运行层的状态优先显示；数据层的通用「连接失败」退居其后，绝不反过来淹没退出码等原因。
    val listNotice = listError?.let { error ->
        localRuntimeNotice?.takeIf { it != error }?.let { "$it\n$error" } ?: error
    }
    // 只有传输层失败才允许出现「连接本机影库」：HTTP 业务错误（401/404/5xx 等）有明确含义，
    // 不能靠切换媒体库掩盖。列表状态在失败时一并记录来源。
    val listErrorIsTransport by remember(listState) {
        derivedStateOf { listState.value.error != null && listState.value.errorIsTransport }
    }
    // 本机端点未 READY：横幅的「重试」必须先启动/重启运行层，单纯 browse 只会再撞空端口。
    val restartLocalRuntime = shouldRestartLocalRuntime(
        model.localRuntimeAvailable, currentIsLocal, localRuntimeStatus.phase)
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
    // 用户下拉刷新标志：只由 PullToRefreshBox 的手势回调置位。重试、批量刷新、服务器推送、
    // 翻页与换 seed 都不置位；成功归位或刷新失败时清零。
    var userRefresh by remember(key) { mutableStateOf(false) }
    LaunchedEffect(key, session, query, group, sceneActive) {
        // 进程恢复会把所有已访问 Tab 一起上树（rememberSaveable visited 集）：隐藏层此时
        // 生命周期是 CREATED，但 LaunchedEffect 不看生命周期。首屏请求只在场景可见时发，
        // 否则恢复详情页时四份列表/统计会同时抢带宽（审查 P1）。
        if (!sceneActive) return@LaunchedEffect
        batch?.bindQuery(query)
        // 入库新鲜度不在这里触发：browse 自己比对 publicationId，进入列表时最多补一次。
        model.browse(key, query)
        val current = query.toString() + group
        if (appliedQuery != current) {
            appliedQuery = current
            // 下拉刷新换 seed 也是一次「查询变化」：此刻屏幕上还是旧列表，立即归位会把旧 key
            // 留给新列表，刷新落地后 Compose 又按该 key 找回索引——正是首页刷新落到未见内容
            // 的原因。用户刷新引起的查询变化交给下方「新列表与新分组都已上屏」的 effect 归位；
            // 其余真实筛选/分区切换保持原行为，立即回顶。
            if (!userRefresh) grid.scrollToItem(0)
        }
    }
    // 回前台补一次新鲜度检查（browse 内部只在 publicationId 落后时才发请求，否则直接返回）。
    // 汇入 100 个文件只会让列表变「旧」，不会在这里变成 100 次请求：刷新发生在下一次
    // 进入列表或回前台，每次至多一次；手动下拉刷新不受影响。
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { model.browse(key, query) }
    // 续页运行距离按"行"给，不再固定 8 格：搜索页的下一页对每个 token 都是一次 instr 全表扫描
    // （server/internal/service/asset_filters.go 的 q 谓词），一次往返数百毫秒。8 格（约 2~3 行）
    // 在快速滑动下等于没有余量：先撞到已加载清单末端停死、再等这一页，每 50 项重复一次。
    val nearEnd by remember(columns) { derivedStateOf {
        val layout = grid.layoutInfo
        val runway = PAGE_RUNWAY_ROWS * columns
        layout.totalItemsCount > 0 && (layout.visibleItemsInfo.lastOrNull()?.index ?: -1) >= layout.totalItemsCount - runway
    } }
    LaunchedEffect(nearEnd, listCursor, query, sceneActive, prefetchEnabled) {
        // 隐藏场景里的布局残影（高度/可见项变化）也能让 nearEnd 成立；不为看不见的网格续页。
        // 首页 pager 的预组合邻页同样不在此续页：它有自己的整页布局，否则会在后台一页页翻下去。
        if (sceneActive && prefetchEnabled && nearEnd && listError == null) model.browse(key, query, next = true)
    }
    // 选择模式/面板开关只派生窄布尔值，勾选单项不会触发浏览区重组。
    if (batch != null) {
        // 隐藏的常驻 Tab 不接收返回键：场景 owner 是 CREATED 时 activity-compose 的
        // lifecycle-aware BackHandler 不会进入 dispatcher，这里再按 LocalSceneActive 显式
        // 收一道，避免任何注册时序把隐藏 Tab 的选择模式接到当前 Tab 的返回键上。
        BackHandler(enabled = sceneActive && selecting && !sheetOpen, onBack = batch::exit)
        // 批量面板只在所属场景可见时渲染：它是独立窗口，触摸死层盖不住（失活时已由上方
        // effect 调 closeSheet()，这里挡住重开与浮空）。
        if (sceneActive && sheetOpen) AlbumBatchSheetHost(batch, selectionState, model::refreshAfterAlbumBatch)
    }
    Column(Modifier.fillMaxSize()) {
        var rows: Map<String, media.qimeng.next.FacetRowState> = emptyMap()
        var filters: (@Composable () -> Unit)? = null
        if (albumTitle != null) {
            // 每个浏览区一份 facet 实例：相册/收藏/历史/作者页共用 Activity 级实例时，换页后
            // 上一页的行会先留下一个往返（FacetViewModel 只比对实例、不比对查询），胶囊计数
            // 就会短暂显示另一页的人群。按 key 分实例后，同一页离开再回来仍在，跨页不再串。
            val facets: media.qimeng.next.FacetViewModel = viewModel(key = "facets:$key")
            val values by facets.rows.collectAsStateWithLifecycle()
            rows = values
            LaunchedEffect(session, query, sceneActive) {
                // 隐藏的已恢复 Tab 不发四行 facet 请求；首次可见时补。
                if (sceneActive) session?.let { facets.bind(it, query) }
            }
            filters = { AlbumFilters(albumTitle, headerTotal, controls, values,
                onBack = if (bottomDock) null else model::back, fetch = facets::fetch, authorKind = authorKind,
                loadFailed = listError != null, columns = columns, showTitle = !selecting,
                onToggleColumns = { setColumns(if (columns >= MAX_GRID_COLUMNS) MIN_GRID_COLUMNS else columns + 1) }) }
            if (batch != null && selecting) AlbumSelectionBarHost(batch, selectionState, entries)
            if (leading == null) filters?.invoke()
        } else if (toolbar) Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp)) {
            listOf(null to "综合", "video" to "视频", "animated_image" to "动图", "image" to "图片").forEach { (value, label) ->
                FilterChip(controls.type.value == value, { controls.type.value = value }, label = { Text(label) })
            }
            IconButton(onClick = { controls.showFilters.value = true }) { Icon(Icons.Outlined.Tune, "筛选") }
            IconButton(onClick = { setColumns(if (columns >= MAX_GRID_COLUMNS) MIN_GRID_COLUMNS else columns + 1) }) { Icon(Icons.Outlined.GridView, "列数") }
        }
        // 筛选面板只在所属场景可见时渲染（失活时已由上方 effect 复位 showFilters）：
        // ModalBottomSheet 是独立窗口，隐藏 Tab 里的实例会浮在当前 Tab 之上。
        if (sceneActive && (albumTitle != null || toolbar)) BrowserFilterSheet(controls)
        val retry: () -> Unit = {
            if (restartLocalRuntime) {
                // 本机端点未 READY：重试必须先拉起运行层并等 READY，单纯 browse 永远撞空端口。
                model.retryLocalRuntime()
            } else {
                // 只有「当前查询确实加载过」才续页；重查失败后 loadedQuery 还是旧查询，
                // 用旧游标续新查询会拿错顺序，改为从第一页重取。
                model.browse(key, query, next = listCursor != null && listState.value.loadedQuery == query, force = true)
            }
        }
        // 远端会话：传输失败时给「连接本机影库」。本机端点未 READY 时上面的重试已改走本机
        // 流程，不再重复摆第二个按钮；运行层 READY 但请求仍失败时保留这个显式重连接口。
        // 本机子进程退出：主按钮仍是「重启本机影库」，第二动作换成需要确认的「重建本机索引」；
        // 远端会话保留原来的「连接本机影库」。两者互斥（重建只在 FAILED+本机时出现）。
        val rebuildLocalIndex = remember(offerIndexRebuild) {
            if (offerIndexRebuild) ErrorNoticeAction("重建本机索引", onClick = { confirmIndexRebuild = true }) else null
        }
        val localRecovery = remember(listErrorIsTransport, session?.baseUrl, localRuntimeAddress,
            localRuntimeStatus.phase, currentIsLocal, restartLocalRuntime) {
            if (!restartLocalRuntime && shouldOfferLocalRecovery(model.localRuntimeAvailable, session?.baseUrl,
                    localRuntimeAddress, listErrorIsTransport, localRuntimeStatus.phase, currentIsLocal))
                ErrorNoticeAction("连接本机影库", model::connectLocalLibrary)
            else null
        }
        val noticeSecondary = rebuildLocalIndex ?: localRecovery
        val retryLabel = if (restartLocalRuntime) "重启本机影库" else "重试"
        if (leading == null) listNotice?.let { ErrorNoticeWithActions(it, retry, retryLabel, noticeSecondary) }
        // 下拉刷新「已是最新」轻提示：只显示这一条一次性文案，其他消息仍走 AppRoot 的
        // pushed 路由提示条，避免同一条消息在两处重复。
        if (route == "main") browserMessage?.takeIf { it == LIST_UP_TO_DATE_MESSAGE }?.let { UpToDateHint(it) }
        val sectionMode = when {
            source == "history" -> "history"
            favorite == true -> "favorite"
            authorKind != null || caption -> "partitions"
            else -> group
        }
        // 分组是 O(n)（groupBy + 组头计数）：页追加/筛选变化时放到 Default 计算，不在页边界
        // 占住当帧主线程。首帧仍在组合内同步算完，网格不会先空一帧；后台结果用引用相等策略
        // 写入（新旧值相等比较本身也是 O(n)，同样不能放主线程）。
        val sectionFacets = group?.let(rows::get)?.items.orEmpty()
        // 分组时钟按浏览实例取一次快照：同一 instance 的每次重算都复用它，跨日/换时区时
        // 「今天」这类标签不会在会话中途翻转（GallerySections 的 nowMillis 契约）。
        val sectionsNow = remember(key) { System.currentTimeMillis() }
        val sectionsState = remember(key) {
            mutableStateOf(
                gallerySections(entries, sectionMode, sectionFacets, dateCounts, unknownDateCount, sectionsNow),
                referentialEqualityPolicy(),
            )
        }
        // 当前渲染的分组描述的是哪一版 entries。异步分组落地前 sectionsOf 仍是旧条目，
        // 归位必须等它追上 entries，否则归位发生在旧列表上，新列表一落地又会按旧 key 找回索引。
        var sectionsOf by remember(key) { mutableStateOf(entries) }
        LaunchedEffect(entries, sectionMode, sectionFacets, dateCounts, unknownDateCount) {
            val computed = withContext(Dispatchers.Default) {
                gallerySections(entries, sectionMode, sectionFacets, dateCounts, unknownDateCount, sectionsNow)
            }
            sectionsState.value = computed
            sectionsOf = entries
        }
        val sections = sectionsState.value
        // 下拉刷新的归位：等新列表真的落地（loading/refreshing 都结束、loadedQuery 属于本查询）
        // 且这批条目的分组也已替换之后才回顶。此时网格里是新内容，索引 0 就是顶部；
        // 归位若对着旧列表发出，Compose 会按旧首项 key 在新顺序里找回索引（首页换 seed 后
        // 落在任意一行）。翻页/服务器推送不置 userRefresh，因此不会触发。
        LaunchedEffect(userRefresh, listLoading, listRefreshing, listError, querySettled, sectionsOf, entries, grid) {
            if (shouldResetScrollAfterRefresh(userRefresh, listLoading, listRefreshing, listError, querySettled, sectionsOf === entries)) {
                userRefresh = false
                grid.scrollToItem(0)
            } else if (userRefresh && listError != null) {
                // 刷新失败：丢弃标志。之后的重试与其它加载不属于这次手势，不应再跳位置。
                userRefresh = false
            }
        }
        // 首页/搜索/收藏/历史与相册一样做视口前瞻预取（旧版 QimengMediaGrid 对全部网格生效）。
        // 带 leading 的网格下标含前导项，预取窗口会错位，故暂不接入。
        // prefetchEnabled=false（首页 pager 的预组合邻页）不启动预取 worker：邻页还没被看到，
        // 不与当前页抢带宽；它的列表登记与第一页数据仍照常进行，滑过去不会先显示骨架。
        if (leading == null && entries.isNotEmpty() && prefetchEnabled) PrefetchGalleryViewport(model, grid, sections, columns)
        PullToRefreshBox(isRefreshing = listRefreshing,
            onRefresh = {
                // 先让全库预取让路，刷新首屏与它在局域网带宽上不再互抢（旧版问题B 同款）。
                model.pauseThumbnailPrefetch()
                // 用户显式下拉：归位等新列表与新分组都上屏（见上方 effect），不在这里提前跳。
                userRefresh = true
                if (query.source == "recommendations" && onRefresh != null) onRefresh()
                // 用户显式下拉：同 id 序列时提示「已是最新」；重试/批量刷新不走这个参数。
                else model.browse(key, query, force = true, announceUnchanged = true)
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
                                        pendingColumns = (pendingColumns + step).coerceIn(MIN_GRID_COLUMNS, MAX_GRID_COLUMNS)
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
                    val message = listNotice
                    when {
                        message != null -> {
                            item(key = "author-error", contentType = "author-status", span = { GridItemSpan(maxLineSpan) }) {
                                Box(Modifier.padding(horizontal = QimengDimens.ScreenPaddingHorizontal, vertical = QimengDimens.SpaceM)) {
                                    ErrorNoticeWithActions(message, retry, retryLabel, noticeSecondary)
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
    // 重建索引会丢弃本机元数据（媒体保留），必须先说明后果并由用户确认；取消不做任何事。
    if (confirmIndexRebuild) AlertDialog(
        onDismissRequest = { confirmIndexRebuild = false },
        title = { Text("重建本机索引") },
        text = { Text("原来的本机索引目录会改名为备份，不会删除；媒体文件保持原样。新实例从空索引开始，本机元数据、标签与整理记录不会带过来，重建后可以重新扫描导入媒体。") },
        confirmButton = {
            TextButton(onClick = { confirmIndexRebuild = false; model.rebuildLocalIndex() }) { Text("重建") }
        },
        dismissButton = { TextButton(onClick = { confirmIndexRebuild = false }) { Text("取消") } },
    )
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

/**
 * 下拉刷新成功但内容未变的轻提示（旧版 HomeScreen.InfoBanner 同款：secondaryContainer、
 * 不可点击；自动消退由 LibraryViewModel 计时，属于成功路径而非错误横幅）。
 */
@Composable
private fun UpToDateHint(message: String) {
    Surface(
        color = MaterialTheme.colorScheme.secondaryContainer,
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
    ) {
        Text(message, Modifier.padding(8.dp), style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSecondaryContainer)
    }
}

/** 网格缩略图圆角沿用旧版 24px（像素密度换算，不是 dp）。 */
private const val TILE_CORNER_RADIUS_PX = 24f

/**
 * 续页运行距离（行）：约 1.3 屏。按行而不是按格，列数变化时余量不变；
 * 搜索的整页延迟可达数百毫秒，这个距离要让一次快滑在页面到达前还留在已加载内容里。
 */
private const val PAGE_RUNWAY_ROWS = 8

/**
 * 这份列表状态是否有可以给 [query] 渲染的内容。重查在途时 `loadedQuery` 仍是上一个查询，
 * 列表条目会被保留：继续渲染它，网格与计数就不会在筛选/刷新期间闪空。请求失败后
 * （error != null）不再沿用上一个查询的条目与计数，否则新筛选会顶着旧结果冒充成功；
 * 同一查询的失败刷新仍由 `loadedQuery == query` 保住内容。
 */
private fun ListState<Asset>.showsDataFor(query: AssetQuery): Boolean =
    loadedQuery == query || (error == null && (loaded || items.isNotEmpty()))

/**
 * 下拉刷新后是否允许把网格归位到顶部（§1 的纯条件，单测锁定）。
 *
 * - [userRefresh]：只由手势回调置位。翻页、批量刷新、服务器推送、换 seed 都不置位，
 *   因此追加与后台补读永远不会触发归位。
 * - [loading]/[refreshing]：请求在途时不动（归位必须落在刷新后的列表上）。
 * - [error]：刷新失败不归位，由调用方丢弃标志。
 * - [settled]：`loadedQuery` 已属于当前查询（换 seed 换查询时旧列表只是暂留显示）。
 * - [sectionsMatch]：渲染中的分组描述的就是当前 entries——异步分组还没换完就归位，会被
 *   随后的分区替换重新锚定（首页换 seed 后「跳一行」的另一半原因）。
 */
internal fun shouldResetScrollAfterRefresh(
    userRefresh: Boolean,
    loading: Boolean,
    refreshing: Boolean,
    error: String?,
    settled: Boolean,
    sectionsMatch: Boolean,
): Boolean = userRefresh && !loading && !refreshing && error == null && settled && sectionsMatch

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
        if (shortTitle) Text(remember(asset.name) { asset.name.substringBeforeLast('.', asset.name) }, Modifier.fillMaxWidth().padding(horizontal = 5.dp), maxLines = 1,
            overflow = TextOverflow.Ellipsis, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
    }
}
