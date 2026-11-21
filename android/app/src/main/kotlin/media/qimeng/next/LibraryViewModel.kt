package media.qimeng.next

import android.app.Application
import android.net.Uri
import android.os.SystemClock
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.model.*
import java.util.UUID

/**
 * 列表状态。[errorIsTransport] 只在传输层失败（连接拒绝、超时、断流）时为 true；
 * 已收到 HTTP 应答的业务错误不能被「连接本机影库」掩盖。
 */
data class ListState<T>(val items: List<T> = emptyList(), val cursor: String? = null,
    val loading: Boolean = false, val loaded: Boolean = false, val error: String? = null,
    val totalMatched: Long? = null, val dateCounts: List<DateCount> = emptyList(), val loadedQuery: AssetQuery? = null,
    val unknownDateCount: Long? = null, val publicationId: String? = null,
    /** 显式刷新（下拉/重试）在途：只有这里驱动刷新指示，筛选重查沿用内容但不亮指示。 */
    val refreshing: Boolean = false,
    val errorIsTransport: Boolean = false)
data class LoadState<T>(val data: T? = null, val loading: Boolean = false, val error: String? = null)
private data class ReactionIntent(val target: Session, val id: String, val revision: Long, val enabled: Boolean)
/** 一次资产页请求的结果；[restarted] 表示推荐顺序过期后已静默重取第一页（见 [LibraryViewModel.browse]）。 */
private data class FetchedPage(val page: Page<Asset>, val restarted: Boolean)

class LibraryViewModel(application: Application, private val saved: SavedStateHandle) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    private val runtime = (application as QimengApplication).runtime
    private val localConnection = (application as QimengApplication).localConnection
    val localRuntimeAvailable = runtime.available
    val localRuntimeStatus = runtime.status
    val localAddress = runtime.configuredAddress
    val localFileAccess = runtime.files.access
    val localScanState = runtime.scans.state
    val defaultEndpoint = graph.settings.defaultEndpoint
    val discovery = graph.discovery.state
    val stagedLoginEndpoint = saved.getStateFlow<String?>("loginEndpoint", null)
    val stagedLoginAddress = saved.getStateFlow<String?>("loginAddress", null)
    val recentServers = graph.settings.recentServers
    val transportRevision = graph.settings.transportRevision
    val localConnectionError = localConnection.error
    private val repository = graph.media
    val session = repository.session
    val preferences = graph.settings.preferences
    val albumColumns = graph.settings.albumColumns
    val homeColumns = graph.settings.homeColumns
    /** 网格列数写入串行化：相册三页与首页两格共用同一把锁，列数提交不会交错。 */
    private val columnWrites = Mutex()
    val settingsError = graph.settings.error
    val queue = graph.queue.state
    val lastPublication = graph.uploads.lastPublication
    val queueError = graph.queue.error
    val route = saved.getStateFlow("route", "main")
    val pendingAssetTools = saved.getStateFlow<String?>("pendingAssetTools", null)
    val tab = saved.getStateFlow("tab", 0)
    val uploadLibrary = saved.getStateFlow("uploadLibrary", "")
    val uploadDirectory = saved.getStateFlow("uploadDirectory", "")
    val uploadMode = saved.getStateFlow("uploadMode", UploadMode.ORDINARY.name)
    val uploadAuthorIds = saved.getStateFlow("uploadAuthorIds", emptyList<String>())
    val uploadCollectionId = saved.getStateFlow<String?>("uploadCollectionId", null)
    val uploadNote = saved.getStateFlow("uploadNote", "")
    val uploadSources = saved.getStateFlow("uploadSources", "")
    val uploadAuthorSources = saved.getStateFlow("uploadAuthorSources", "")
    val uploadFamily = saved.getStateFlow("uploadFamily", "")
    val uploadAutoName = saved.getStateFlow("uploadAutoName", false)
    val editedStems = saved.getStateFlow("uploadStems", HashMap<String, String>())
    private val nameWrites = mutableMapOf<String, Job>()
    private val mutableMessage = MutableStateFlow<String?>(null)
    val message = mutableMessage.asStateFlow()
    /**
     * 当前全局提示是否同时提供「连接本机影库」。传输层失败、或当前会话就是本机端点且运行层
     * 未 READY 时为 true；远端已收到 HTTP 应答的业务错误与已在屏上的旧提示不带来这个动作。
     */
    private val mutableMessageOffersLocalRecovery = MutableStateFlow(false)
    val messageOffersLocalRecovery = mutableMessageOffersLocalRecovery.asStateFlow()
    /** 一次性轻提示的票据：新提示递增，旧定时器据此不再清掉新消息。 */
    private var hintTicket = 0L
    /**
     * 连接失败提示的冷却闸门：同一文本已在屏上或 4s 内刚发过就不再重发。
     * 后台健康检查、自动恢复与多次详情刷新会连续撞上同一个断线窗口；没有闸门时
     * 每次失败都重post，横幅看起来「反复出现」。成功路径清空提示时一并重置。
     */
    private val connectionMessages = ConnectionMessageGate()
    private val mutableBusy = MutableStateFlow(false)
    val busy = mutableBusy.asStateFlow()
    private val libraryState = MutableStateFlow(ListState<Library>())
    val libraryPage = libraryState.asStateFlow()
    val libraries = libraryState.map { it.items }.stateIn(viewModelScope, SharingStarted.Eagerly, emptyList())
    private val mutableCapabilities = MutableStateFlow<ServerCapabilities?>(null)
    val capabilities = mutableCapabilities.asStateFlow()
    private val mutableAsset = MutableStateFlow<Asset?>(null)
    private val reactionRetries = mutableMapOf<String, Pair<ReactionIntent, String>>()
    val asset = mutableAsset.asStateFlow()
    private val mutableAuthor = MutableStateFlow<Author?>(null)
    val author = mutableAuthor.asStateFlow()
    private val mutableAuthorFollow = MutableStateFlow(LoadState<String>())
    val authorFollow = mutableAuthorFollow.asStateFlow()
    private val mutableAssetAuthors = MutableStateFlow<List<Author>>(emptyList())
    val assetAuthors = mutableAssetAuthors.asStateFlow()
    private val mutableReport = MutableStateFlow<TaskReport?>(null)
    val report = mutableReport.asStateFlow()
    private val mutableStats = MutableStateFlow<InventoryStats?>(null)
    val statistics = mutableStats.asStateFlow()
    private val analyticsState = MutableStateFlow(LoadState<AnalyticsReport>())
    val analytics = analyticsState.asStateFlow()
    private val reviewState = MutableStateFlow(LoadState<BrowsingReview>())
    val review = reviewState.asStateFlow()
    private val recommendationState = MutableStateFlow<RecommendationPreferences?>(null)
    val recommendationPreferences = recommendationState.asStateFlow()
    private var analyticsJob: Job? = null
    private var reviewJob: Job? = null
    private var rankingJob: Job? = null
    private var rankingQuery: String? = null
    private val rankingState = MutableStateFlow(ListState<ActivityRank>())
    val ranking = rankingState.asStateFlow()
    private val mutableTags = MutableStateFlow<List<String>>(emptyList())
    val tagVocabulary = mutableTags.asStateFlow()
    private val mutableTagCursor = MutableStateFlow<String?>(null)
    val tagCursor = mutableTagCursor.asStateFlow()
    private val tagReads = RequestGeneration()
    private val lists = mutableMapOf<String, MutableStateFlow<ListState<Asset>>>()
    /** 组合期只读入口在 key 尚未登记时返回的共享空状态：不参与写入，也不带任何分页任务。 */
    private val emptyAssetsState = MutableStateFlow(ListState<Asset>())
    private val queries = mutableMapOf<String, AssetQuery>()
    private val listJobs = mutableMapOf<String, Job>()
    private val listVersions = mutableMapOf<String, RequestGeneration>()
    /** 每个浏览 key 连续「0 条 + 非空游标」的页数；见 [MAX_EMPTY_APPEND_PAGES]。 */
    private val emptyAppends = mutableMapOf<String, Int>()
    /** 已翻过页的浏览 key：同查询的入库补查只并入第一页，不把清单缩回第一页（见 [browse]）。 */
    private val paginatedKeys = mutableSetOf<String>()
    private val authorState = MutableStateFlow(ListState<Author>())
    val authors = authorState.asStateFlow()
    private val reportState = MutableStateFlow(ListState<TaskReport>())
    val reports = reportState.asStateFlow()
    private val authorTaskState = MutableStateFlow(ListState<AuthorTaskReport>())
    val authorTasks = authorTaskState.asStateFlow()
    private val trashState = MutableStateFlow(ListState<Asset>())
    val trash = trashState.asStateFlow()
    private val collectionState = MutableStateFlow(ListState<MediaCollection>())
    val collections = collectionState.asStateFlow()
    private var collectionJob: Job? = null
    private var detailJob: Job? = null
    private var detailGeneration = RequestGeneration()
    private var connectionGeneration = 0L
    private val activeOperations = mutableSetOf<Job>()
    private val pageJobs = mutableMapOf<MutableStateFlow<*>, Job>()
    /** 与服务端事件计数配对：同一会话上的后续计数才算真实推送（见 [ServerEventSessionGate]）。 */
    private val serverEventGate = ServerEventSessionGate()

    init {
        viewModelScope.launch {
            session.collect { connection ->
                // 会话切换：下一次计数只当作重连（见 ServerEventSessionGate），不能拿旧会话
                // 已加载的列表去补读——即便重新登录的是同一台服务器，列表也已在上方清空。
                serverEventGate.forget()
                connectionGeneration++
                activeOperations.toList().forEach(Job::cancel)
                mutableBusy.value = false
                listJobs.values.forEach(Job::cancel)
                emptyAppends.clear()
                paginatedKeys.clear()
                pageJobs.values.forEach(Job::cancel); pageJobs.clear()
                detailJob?.cancel()
                detailGeneration.next()
                reactionRetries.clear()
                lists.values.forEach { it.value = ListState() }
                mutableAsset.value = null; mutableAuthor.value = null; mutableAuthorFollow.value = LoadState()
                mutableStats.value = null; mutableReport.value = null
                discardForeignObjectRoutes(connection)
                mutableAssetAuthors.value = emptyList()
                tagReads.next()
                mutableTags.value = emptyList(); mutableTagCursor.value = null
                libraryState.value = ListState(); mutableCapabilities.value = null
                analyticsJob?.cancel(); reviewJob?.cancel(); collectionJob?.cancel(); rankingJob?.cancel()
                rankingState.value = ListState(); rankingQuery = null
                collectionState.value = ListState()
                if (saved.get<String>("uploadInstance") != connection?.instanceId) {
                    saved["uploadAuthorIds"] = emptyList<String>(); saved["uploadCollectionId"] = null; saved["uploadSources"] = ""; saved["uploadFamily"] = ""; saved["uploadAutoName"] = false
                    saved["uploadDirectory"] = ""; saved["uploadNote"] = ""; saved["uploadAuthorSources"] = ""
                }
                saved["uploadInstance"] = connection?.instanceId
                analyticsState.value = LoadState(); reviewState.value = LoadState(); recommendationState.value = null
                authorState.value = ListState(); reportState.value = ListState()
                authorTaskState.value = ListState()
                trashState.value = ListState()
                if (connection != null) {
                    saved["loginEndpoint"] = null; saved["loginAddress"] = null; saved["loginLocalAddress"] = null
                    saved["uploadLibrary"] = graph.settings.uploadLibrary(connection).orEmpty()
                    refreshConnection()
                }
            }
        }
        // 服务端事件流（core/data 的 ServerEventConsumer）只广播「哪类数据变旧了」：library /
        // favorite / like 是单调计数，合并成一个数后任意一类变化都做同一件事——静默重读已有
        // 内容的列表与打开中的详情。distinctUntilChanged 消除三类计数在同一轮里相加后不变
        // 的情况；drop(1) 丢掉 combine 的初始值（三个 0），不为启动补读。
        // 会话闸门：连接（重新）建立时流会把三类计数各补一次（断线期间的事件无法重放），
        // 那一次必须丢弃——此刻列表状态还属于旧会话，拿它 browse 会把旧查询发到新服务器。
        viewModelScope.launch {
            combine(graph.serverEvents.signals.library, graph.serverEvents.signals.favorite, graph.serverEvents.signals.like) { library, favorite, like ->
                library + favorite + like
            }.distinctUntilChanged().drop(1).collect {
                if (serverEventGate.accept(session.value)) refreshAfterServerEvent()
            }
        }
    }

    /**
     * 服务器推送后的静默补读：只重读已经有内容的列表，以及打开中的详情。未加载的列表由
     * 各页面自己的首载负责（这里补读也只会让骨架多一次请求）。全部走 silent：不亮下拉刷新
     * 指示、不触发 §1 的滚动归位——后台推送不应挪动用户正在看的位置。
     */
    private fun refreshAfterServerEvent() {
        if (session.value == null) return
        lists.forEach { (key, state) ->
            if (state.value.loaded) browse(key, force = true, silent = true)
        }
        val open = route.value
        if (open.startsWith("asset:")) refreshAsset(open.substringAfter(':'))
    }

    /**
     * 组合期只读查询：只回已有列表流，不创建、不淘汰。缺失时返回共享空流（首帧即骨架），
     * 由 [openAssets] 在 LaunchedEffect 里登记，避免在组合中改写缓存或淘汰正在显示的列表
     * （淘汰会连带取消它的分页任务）。
     */
    fun assets(key: String): StateFlow<ListState<Asset>> = lists[key] ?: emptyAssetsState

    /** 非组合调用：登记 key 并返回稳定列表流（动态 key 超量时淘汰最旧一条）。 */
    fun openAssets(key: String): StateFlow<ListState<Asset>> = assetState(key)

    /**
     * 最近一次权威匹配总数，按「实例 + 浏览 key + 查询身份」保留。列表状态本身在换筛选/
     * 重新进入时保留计数（见 [browse] 的 keep 语义），但列表被淘汰（author:/collection:
     * 超过上限）或换实例后重建时计数随之消失；胶囊旁的计数不允许因此回退成空白，故另存一份。
     * 换了筛选就是另一组人群，旧数字不能冒充新查询（[screen.CountSeeds] 同一条规则），
     * 因此查询身份必须进键，而不只是浏览 key。只在响应成功之后写入，绝不写入推测值。
     */
    private val carriedTotals = mutableMapOf<Triple<String, String, String>, Long>()

    /**
     * 查询身份：数据类完整字段的稳定文本；不用 hashCode，避免不同筛选相撞后串用计数。
     * 稳定性依赖「AssetQuery 里没有无序集合」：当前字段均为 String/Boolean?/Long/Int?/List<String>，
     * List 的 toString 有序；以后若引入 Set/Map/数组，必须在这里改成有序/规范化渲染，
     * 否则无序的 toString 会让同一查询得到不同键，计数互相串用。
     */
    private fun countScope(query: AssetQuery): String = query.toString()

    /** 组合期只读：该实例该 key 该查询上一次成功的匹配总数；从未成功加载时为 null（不伪造 0）。 */
    fun carriedTotal(key: String, query: AssetQuery): Long? =
        carriedTotals[Triple(connectionInstance(), key, countScope(query))]

    /** 记录权威匹配总数（[screen.MediaBrowser] 在响应落地后回写；只写本次查询的数字）。 */
    fun carryTotal(key: String, query: AssetQuery, total: Long?) {
        if (total != null) carriedTotals[Triple(connectionInstance(), key, countScope(query))] = total
    }

    /**
     * 媒体库索引已完整加载时，无筛选资产查询的总数 = 启用库 fileCount 之和：服务端
     * libraryProjection 的 fileCount 与 GET /assets 默认谓词同为「trashed=0 且库 enabled」，
     * 因此这是导航前就已拿到的真实计数，可在相册首屏响应落地前直接显示。索引尚未加载或
     * 仍有下一页时返回 null——不拿残缺清单求和，也不打印 0。
     */
    fun libraryIndexTotal(): Long? {
        val page = libraryState.value
        if (!page.loaded || page.cursor != null) return null
        return page.items.filter { it.enabled }.sumOf { it.fileCount }
    }

    private fun connectionInstance(): String = session.value?.instanceId.orEmpty()

    fun saveAlbumColumns(columns: Int) = saveColumns(columns) { graph.settings.saveAlbumColumns(it) }

    /** 首页推荐/COS 两格的列数落盘（与相册列数同一把写锁、同一保存纪律）。 */
    fun saveHomeColumns(columns: Int) = saveColumns(columns) { graph.settings.saveHomeColumns(it) }

    private fun saveColumns(columns: Int, write: suspend (Int) -> Unit) {
        viewModelScope.launch {
            try {
                columnWrites.withLock {
                    withContext(Dispatchers.IO) { write(columns) }
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { showFailure(failure) }
        }
    }

    suspend fun prefetchNearbyThumbnails(assets: List<Asset>): Boolean = graph.thumbnails.prefetchNearby(assets)

    /**
     * 前台刷新让路（对齐旧版 ThumbnailPrefetchThrottle）：全库预取在窗口内暂停下载，
     * 首屏/刷新请求独占局域网带宽。窗口有界且自愈，不需要配对的恢复调用。
     */
    fun pauseThumbnailPrefetch() = graph.thumbnails.pauseForForegroundRefresh()

    private fun assetState(key: String): MutableStateFlow<ListState<Asset>> {
        lists[key]?.let { return it }
        val dynamic = lists.keys.filter { it.startsWith("author:") || it.startsWith("collection:") }
        if ((key.startsWith("author:") || key.startsWith("collection:")) && dynamic.size >= 12) {
            val expired = dynamic.first()
            listJobs.remove(expired)?.cancel(); lists.remove(expired); queries.remove(expired); listVersions.remove(expired)
        }
        return MutableStateFlow(ListState<Asset>()).also { lists[key] = it }
    }
    fun query(key: String): AssetQuery = queries[key] ?: AssetQuery()
    fun cancelBrowse(key: String) {
        listJobs.remove(key)?.cancel()
        listVersions[key]?.next()
        lists[key]?.let { state ->
            // 取消在途请求不等于丢弃已有结果：返回列表页时仍显示上次加载的条目与计数，
            // 骨架只留给从未拿到数据的列表。需要作废数据的调用方（批量保存/收藏变更）
            // 自行把 loaded 置假。
            if (state.value.loading || state.value.refreshing)
                state.value = state.value.copy(loading = false, refreshing = false)
        }
    }
    fun clearSearchResults() {
        cancelBrowse("search")
        assetState("search").value = ListState()
    }
    fun refreshAfterAlbumBatch(target: Session) {
        if (!sameTarget(target, session.value)) return
        lists.forEach { (key, state) ->
            cancelBrowse(key)
            state.value = state.value.copy(loaded = false)
        }
        browse("albums", force = true)
    }
    /**
     * 加载一页资产。[announceUnchanged] 只由用户显式下拉刷新传入：同一查询的强制首屏刷新
     * 返回与屏幕完全相同的 id 序列时，经 [mutableMessage] 提示一次「已是最新」；
     * 翻页、首载、筛选变化与后台补查都不提示。
     *
     * [silent] 只由服务器推送补读传入：不点亮下拉刷新指示（[ListState.refreshing]），
     * 因此界面不会把它当作一次用户刷新去做滚动归位；已翻页的清单同时沿用「新鲜度补查」
     * 的并入语义，不把用户已经滚到的页数截回第一页。
     */
    fun browse(key: String, query: AssetQuery = query(key), next: Boolean = false, force: Boolean = false,
               announceUnchanged: Boolean = false, silent: Boolean = false) {
        val state = assetState(key)
        val previous = queries[key]
        val publication = lastPublication.value?.takeIf { sameTarget(it.first, session.value) }?.second
        // 入库（publication）只作「旧」标记：翻页不因新发布而被拦下（那会让列表停更），
        // 也不在翻页时把标记认作最新——页面本身由下一次进入列表/回前台的补查刷新。
        if (next && (state.value.loading || state.value.cursor == null || state.value.loadedQuery != query)) return
        if (!next && !force && previous == query && state.value.publicationId == publication && (state.value.loaded || state.value.loading)) return
        queries[key] = query
        listJobs[key]?.cancel()
        val generation = listVersions.getOrPut(key) { RequestGeneration() }
        val ticket = generation.next()
        val connection = connectionGeneration
        val old = state.value
        // 仅因入库标记过期而补查（查询未变、也不是显式刷新）：旧清单仍是同一查询的结果，
        // 已翻页的列表在响应落地时并入第一页而不是被替换成 50 条（见下方 mergeFresh）。
        val freshnessOnly = !next && !force && previous == query && old.publicationId != publication
        // 重新查询不等于丢弃现有内容：旧版在筛选/分区/刷新重查期间把 isLoading 叠在现有状态上，
        // 条目与计数保持可见，只在响应落地时整页替换。只有从未加载过的列表才新建空状态（骨架）。
        val keep = next || previous == query || old.loaded || old.items.isNotEmpty()
        // 只有显式刷新（下拉/重试）与首页换 seed 才亮刷新指示；普通筛选重查沿用内容、不亮指示。
        // 入库标记过期的同查询补查（freshnessOnly）是静默维护，同样不亮用户没触发的刷新指示；
        // 服务器推送补读（silent）也是后台维护，用户没有下拉动作。
        val refreshing = !silent && !next && !freshnessOnly && (force || (previous != null && previous.copy(seed = query.seed) == query))
        // 只有用户显式下拉传入 announceUnchanged 才比较刷新前后；重试、换 seed 与后台补查都不算。
        val reportUnchanged = announceUnchanged && !next && force
        // 翻页沿用原发布标记（尚未按最新发布补齐），首屏/刷新才认领当前发布。
        val requestPublication = if (next) old.publicationId else publication
        state.value = if (keep) old.copy(loading = true, refreshing = refreshing, error = null, errorIsTransport = false, publicationId = requestPublication)
            else ListState(loading = true, publicationId = requestPublication)
        listJobs[key] = viewModelScope.launch {
            try {
                // 翻页瞬时失败按 APPEND_RETRY_DELAYS_MS 退避重试；首载/刷新/筛选变化立即失败。
                // 推荐顺序过期仍静默从第一页重取，退避重试不重置该分支（见 requestAssetPage）。
                val fetched = requestAssetPage(query, if (next) old.cursor else null, next) {
                    generation.isCurrent(ticket) && connection == connectionGeneration
                } ?: return@launch
                val page = fetched.page
                val append = next && !fetched.restarted
                if (generation.isCurrent(ticket) && connection == connectionGeneration) {
                    // 过期游标静默重取第一页后仍然**并入**现有清单：把屏幕上的 N 条缩成 50 条
                    // 会在用户钉在底部时抽走内容、把滚动位置钉到新尾部（旧版换轮追加=去重后
                    // 并入，同语义；游标随新顺序走，后续翻页继续）。
                    // 入库后的同查询补查同理：已翻页的清单把第一页按 id 并入（现有条目在前，
                    // 新 id 追加到末尾）——条目顺序不变，网格不会跳回顶部；未翻页的列表仍整页替换。
                    // old.loadedQuery == query 保证并入的旧条目确实属于本次查询（上一次筛选失败时
                    // 清单仍停在上一个查询，不能借入库补查把旧人群的条目混进新查询）。
                    // old.loaded 排除被有意作废的清单（批量改作者/来源会把 loaded 置假）：
                    // 那些旧条目已经不属于新人群，必须整页替换而不是并入。
                    // 服务器推送的 silent 补读同样并入：它可能发生在用户滚到第 5 页时，整页替换
                    // 会把清单截回第一页并把位置钉到新尾部（后台推送不应挪动阅读位置）。
                    // totalMatched 仅首屏返回，翻页继承已有非空总数，避免页头计数被 null 冲掉。
                    // 空页连发保护（旧版 HomeViewModel.MAX_EMPTY_SEED_ROUNDS=3 同口径）：服务端
                    // 可能按页过滤掉已回收/停用项而返回「0 条 + 非空游标」，哨兵会立刻再要下一页；
                    // 连续 MAX_EMPTY_APPEND_PAGES 个空页后把游标视为到底，停手而不是成串发请求。
                    val emptyStreak = if (next && !fetched.restarted && page.items.isEmpty()) (emptyAppends[key] ?: 0) + 1 else 0
                    if (emptyStreak == 0) emptyAppends.remove(key) else emptyAppends[key] = emptyStreak
                    val exhausted = next && !fetched.restarted && page.items.isEmpty() && emptyStreak >= MAX_EMPTY_APPEND_PAGES
                    // 空页不并入：第一页为空说明匹配项可能已被回收/停用（totalMatched=0），
                    // 此时保留旧条目会让网格与页头计数互相矛盾；空页走整页替换，两者一起归零。
                    val mergeFresh = (freshnessOnly || (silent && !next)) && page.items.isNotEmpty() && old.loaded && old.loadedQuery == query && key in paginatedKeys
                    val merged = append || fetched.restarted || mergeFresh
                    // 整页替换（换筛选/显式刷新）后列表重新从第一页开始，翻页标记随之作废。
                    if (next || mergeFresh) paginatedKeys.add(key) else paginatedKeys.remove(key)
                    // 并入是 O(n) 的 id 去重：页追加时放到 Default，避免在页边界占住主线程。
                    // 挂起期间可能已开始新查询，因此写回前必须重新校验代次与连接。
                    val mergedItems = if (merged) {
                        withContext(Dispatchers.Default) { appendDistinct(old.items, page.items) }
                    } else page.items
                    if (!generation.isCurrent(ticket) || connection != connectionGeneration) return@launch
                    // 下拉刷新成功但首屏 id 序列与屏幕上的完全一致：不重建内容，只轻提示一次。
                    val unchangedRefresh = reportUnchanged && old.loaded && old.loadedQuery == query &&
                        sameAssetIds(old.items, page.items)
                    state.value = ListState(mergedItems,
                        // 新鲜度并入沿用原游标：从用户停下的位置继续翻页，不把第 2..N 页重走一遍。
                        when {
                            exhausted -> null
                            mergeFresh -> old.cursor
                            else -> page.nextCursor
                        }, loaded = true, totalMatched = page.totalMatched ?: old.totalMatched.takeIf { next || mergeFresh },
                        dateCounts = if (next) old.dateCounts else page.dateCounts, loadedQuery = query,
                        unknownDateCount = if (next) old.unknownDateCount else page.unknownDateCount, publicationId = requestPublication)
                    if (unchangedRefresh) showUpToDateHint()
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (generation.isCurrent(ticket) && connection == connectionGeneration)
                    state.value = state.value.copy(loading = false, refreshing = false, error = explain(failure),
                        errorIsTransport = isTransportFailure(failure))
            }
        }
    }

    /**
     * 取一页资产。[append] 失败按 [APPEND_RETRY_DELAYS_MS] 退避重试（旧版 APPEND_RETRY_DELAYS_MS
     * 的「翻页失败即卡死」修复）；首载/显式刷新/筛选变化立即失败。推荐顺序过期仍静默从第一页
     * 重取，且不消耗重试次数。[stillCurrent] 在每次退避后校验代次与连接，失效即放弃（返回 null），
     * 调用方不写任何状态。
     */
    private suspend fun requestAssetPage(
        query: AssetQuery,
        cursor: String?,
        append: Boolean,
        stillCurrent: () -> Boolean,
    ): FetchedPage? {
        var restarted = false
        var retryAttempt = 0
        while (true) {
            try {
                val page = repository.assets(query, if (restarted) null else cursor)
                return FetchedPage(page, restarted)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (failure: Exception) {
                if (!restarted && append && failure is ApiFailure && failure.code == "recommendation_expired") {
                    restarted = true
                    continue
                }
                val retryDelay = appendRetryDelayMs(append, retryAttempt) ?: throw failure
                retryAttempt++
                delay(retryDelay)
                // 退避期间刷新/筛选/换连接已接管：直接退出，不把残局写回状态。
                if (!stillCurrent()) return null
            }
        }
    }

    /**
     * 刷新成功但内容未变的一次性轻提示（旧版 HomeUiState.infoMessage 口径：2.5 秒自动消退）。
     * 用户手动关闭、导航清空或期间出现新提示时，旧定时器不得误清。
     */
    private fun showUpToDateHint() {
        val ticket = ++hintTicket
        // 刷新成功本身就是「连接正常」的证据：下一次真实失败应立刻提示，不受旧冷却影响。
        connectionMessages.reset()
        publishMessage(LIST_UP_TO_DATE_MESSAGE)
        viewModelScope.launch {
            delay(UP_TO_DATE_HINT_MS)
            if (ticket == hintTicket && mutableMessage.value == LIST_UP_TO_DATE_MESSAGE) publishMessage(null)
        }
    }

    fun selectTab(index: Int) { saved["tab"] = index; saved["route"] = "main"; saved["backStack"] = arrayListOf<String>() }
    fun navigate(destination: String) {
        if (destination != route.value) saved["backStack"] = ArrayList(saved.get<ArrayList<String>>("backStack").orEmpty() + route.value)
        saved["route"] = destination
        clearMessageNow()
    }
    fun back() {
        detailGeneration.next(); detailJob?.cancel()
        val stack = saved.get<ArrayList<String>>("backStack").orEmpty()
        saved["route"] = stack.lastOrNull() ?: "main"
        saved["backStack"] = ArrayList(stack.dropLast(1))
    }
    fun openTagSearch(tag: String) {
        val target = session.value ?: return
        if (bindObjectTarget(target)) navigate("tag:$tag")
    }

    fun refreshConnection() {
        val generation = connectionGeneration
        val target = session.value
        viewModelScope.launch {
            // 服务端可能仍在冷启动/重启（内嵌本机或 PC/NAS 刚起）：先做就绪等待再发业务
            // 请求，避免把首个 capabilities() 打进未监听窗口后立刻报「连接失败」。
            // 探针超时不改变失败语义：下面照常重试 3 次，仍失败则走后台维护的静默路径。
            if (target != null) {
                repository.awaitReady(target)
                if (generation != connectionGeneration) return@launch
            }
            var failure: Exception? = null
            repeat(3) { attempt ->
                try {
                    val caps = repository.capabilities()
                    if (generation != connectionGeneration) return@launch
                    mutableCapabilities.value = caps
                    loadLibraries()
                    graph.uploads.schedule()
                    session.value?.let { graph.archives.resume(it) }
                    // 本机服务刚启动或刚迁移地址时可能短暂连不上；成功后要清掉旧的失败横幅。
                    clearMessageNow()
                    graph.queue.clearConnectionProblem()
                    return@launch
                } catch (cancelled: CancellationException) { throw cancelled }
                catch (error: Exception) {
                    failure = error
                    if (attempt < 2) delay(600L * (attempt + 1))
                }
            }
            // 刷新连接属于后台维护：失败时由各页面自己的空状态/错误卡呈现，
            // 不再往首页顶栏塞全局提示。
            if (generation == connectionGeneration) clearMessageNow()
        }
    }
    /** 服务版本按「会话 + 传输修订」缓存：同一连接重进「我的」先显已知值，不再先读后显。 */
    private var serverVersionCache: Triple<Session, Long, String>? = null
    fun cachedServerVersion(target: Session, transport: Long): String? = serverVersionCache
        ?.takeIf { it.first == target && it.second == transport }?.third
    suspend fun readServerVersion(target: Session, transport: Long): LoadState<String> = try {
        val version = repository.serverVersion(target)
        check(sameTarget(target, session.value) && transport == transportRevision.value) { "连接已改变" }
        if (version != null) serverVersionCache = Triple(target, transport, version)
        LoadState(data = version)
    } catch (cancelled: CancellationException) { throw cancelled }
    catch (failure: ApiFailure) { LoadState(error = failure.message) }
    catch (_: java.io.IOException) { LoadState(error = "未连接到服务") }
    catch (_: IllegalStateException) { LoadState(error = "连接已改变，请重新登录") }
    catch (_: Exception) { LoadState(error = "无法读取服务版本") }
    fun loadLibraries(next: Boolean = false) = loadPage(libraryState, next) { cursor ->
        val page = repository.libraryPage(cursor)
        if (cursor != null) {
            val visible = libraryState.value.items.mapTo(mutableSetOf(), Library::id)
            return@loadPage page.copy(items = page.items.filterNot { it.id in visible })
        }
        if (uploadLibrary.value.isEmpty() && page.nextCursor == null && page.items.count { it.enabled } == 1) {
            persistUploadLibrary(page.items.single { it.enabled }.id)
        }
        val target = session.value
        val selected = uploadLibrary.value
        if (target != null && selected.isNotEmpty() && page.nextCursor != null && page.items.none { it.id == selected }) {
            val savedLibrary = try { repository.library(target, selected) } catch (failure: ApiFailure) {
                if (failure.status != 404) throw failure
                null
            }
            if (savedLibrary != null) return@loadPage page.copy(items = listOf(savedLibrary) + page.items)
        }
        page
    }

    fun login(address: String, password: String, expectedInstanceId: String? = null) = action {
        repository.loginWithPassword(address, password, expectedInstanceId = expectedInstanceId, loginEndpoint = "nas")
    }
    fun loginLocal(password: String = "") = action {
        saved.get<String>("loginLocalAddress")?.let { runtime.configureAddress(it) }
        connectLocal(forceLogin = true, password = password)
    }
    fun discoverServers() = graph.discovery.start(graph.settings.recentServers.value)
    fun stopDiscovery() = graph.discovery.stop()
    fun requestLocalFileAccess() = action { runtime.files.requestAccess() }
    fun refreshLocalFileAccess() = runtime.files.refreshAccess()
    /** 会话地址是否由本机运行层拥有（私有启动回执确认过的实例与端口）。 */
    fun isLocalEndpoint(target: Session?): Boolean =
        target != null && runtime.available && target.baseUrl in runtime.ownedEndpoints(target.instanceId)
    fun isLocalSession() = isLocalEndpoint(session.value)
    fun setChargeOnlyScan(enabled: Boolean) = action { runtime.scans.setChargeOnly(enabled) }
    fun saveDefaultEndpoint(value: String) = action(blocking = false) {
        check(value != "local" || runtime.available) { "此安装包未包含本机服务" }
        withContext(Dispatchers.IO) { graph.settings.saveDefaultEndpoint(value) }
    }
    fun prepareLogin(endpoint: String, address: String? = null) = action {
        val normalized = if (endpoint == "local") runtime.validateAddress(address ?: runtime.configuredAddress.value)
            else address?.takeIf { it.isNotBlank() }?.let { media.qimeng.next.data.normalizedServer(it) }
        withContext(Dispatchers.IO) { graph.settings.saveDefaultEndpoint(endpoint) }
        saved["loginEndpoint"] = endpoint
        if (endpoint == "local") saved["loginLocalAddress"] = normalized else saved["loginAddress"] = normalized
        saved["route"] = "main"; saved["backStack"] = ArrayList<String>()
        repository.disconnect()
    }
    fun stopLocal() = action { runtime.stop() }
    fun restoreLocalConnection() {
        runtime.files.refreshAccess()
        session.value?.let { graph.backups.resume(it) }
        // 回前台自动恢复：失败已经由本机传输状态与页面空态呈现，不再弹全局横幅。
        // 非连接类失败（凭据/协议/业务校验）仍然照常提示，不能被这里吞掉。
        if (runtime.available && runtime.shouldRestore && session.value?.baseUrl == runtime.baseUrl) {
            action(surfaceConnectionFailure = false) { connectLocal(forceLogin = false) }
        }
    }
    private suspend fun connectLocal(forceLogin: Boolean, password: String = "", retryLocal: Boolean = false) {
        localConnection.connect(forceLogin, password, retryLocal)
        refreshConnection()
    }

    /**
     * 连接失败横幅在本机端点上的「重启本机影库」：不重新登录，只按既有本机连接流程启动/
     * 重启运行层、等待 READY 后刷新连接。用户点按才执行；自动恢复仍尊重显式停止标记。
     */
    fun retryLocalRuntime() = action {
        check(localRuntimeAvailable) { "此安装包未包含本机服务" }
        connectLocal(forceLogin = false, retryLocal = true)
        refreshFailedLists()
    }

    /**
     * 连接失败横幅上的「连接本机影库」：显式启动本机运行模块并按正常会话交换登录。
     * 只有用户点按才会调用；失败时不自动切换媒体库，也不在后台拉起本机服务。
     */
    fun connectLocalLibrary() = action {
        check(localRuntimeAvailable) { "此安装包未包含本机服务" }
        connectLocal(forceLogin = true)
        refreshFailedLists()
    }

    /**
     * 连接恢复后重读当前失败的列表：同一端点的会话重新保存时 Session 相等，StateFlow 不发
     * 新值，列表的 LaunchedEffect 不会重跑；不主动重读，横幅会在连接已恢复后继续挂着。
     */
    private fun refreshFailedLists() {
        lists.keys.toList().filter { lists[it]?.value?.error != null }
            .forEach { key -> browse(key, query(key), force = true) }
    }
    fun logout() = action { repository.logout(); saved["route"] = "main" }
    fun disconnect() = action { repository.disconnect(); saved["route"] = "main" }
    fun clearMessage() { clearMessageNow() }

    /** 发布全局提示；除 [showFailure] 的传输失败分支外，所有提示都不带本机恢复动作。 */
    private fun publishMessage(text: String?) {
        mutableMessage.value = text
        mutableMessageOffersLocalRecovery.value = false
    }

    /** 清空提示并重置连接失败冷却：成功/用户关闭/新动作开始都视为「上一轮失败已结束」。 */
    private fun clearMessageNow() {
        publishMessage(null)
        connectionMessages.reset()
    }

    /**
     * 失败提示的唯一出口：连接类文本先过冷却闸门，其余文本（业务校验、权限、冲突等）
     * 原样发布。见 [shouldPostConnectionMessage]。传输层失败或本机运行层未 READY 时才可能
     * 附带「连接本机影库」；远端已收到 HTTP 应答的业务错误不能靠切换媒体库掩盖。
     */
    private fun showFailure(failure: Exception) {
        val text = explain(failure)
        if (connectionMessages.shouldPost(text, mutableMessage.value, monotonicNowMs())) {
            mutableMessage.value = text
            val current = session.value
            mutableMessageOffersLocalRecovery.value = shouldOfferLocalRecovery(
                localRuntimeAvailable, current?.baseUrl, runtime.baseUrl, isTransportFailure(failure),
                runtime.status.value.phase, isLocalEndpoint(current))
        }
    }

    private fun ownsObjectRoutes(target: Session) =
        saved.get<String>("objectInstance") == target.instanceId && saved.get<String>("objectEndpoint") == target.baseUrl

    private fun discardForeignObjectRoutes(target: Session?) {
        if (target != null && ownsObjectRoutes(target)) return
        fun objectRoute(value: String) = value.startsWith("asset:") || value.startsWith("asset-edit:") ||
            value.startsWith("task:") || value.startsWith("tag:")
        saved["pendingAssetTools"] = null
        val stack = saved.get<ArrayList<String>>("backStack").orEmpty()
        val retained = stack.filterNot(::objectRoute)
        if (objectRoute(route.value)) {
            saved["route"] = retained.lastOrNull() ?: "main"
            saved["backStack"] = ArrayList(retained.dropLast(1))
        } else if (retained.size != stack.size) saved["backStack"] = ArrayList(retained)
    }

    private fun bindObjectTarget(target: Session): Boolean {
        if (!sameTarget(target, session.value)) {
            publishMessage("请先连接任务原来的服务器")
            return false
        }
        discardForeignObjectRoutes(target)
        saved["objectInstance"] = target.instanceId
        saved["objectEndpoint"] = target.baseUrl
        return true
    }

    private fun targetForRefresh(destination: String) = session.value?.takeIf { route.value == destination && ownsObjectRoutes(it) }

    fun refreshAsset(id: String) { targetForRefresh("asset:$id")?.let { openAsset(id, target = it) } }

    fun targetForAssetEditor(id: String): Session? = targetForRefresh("asset-edit:$id")

    fun openAssetEditor(asset: Asset) {
        if (route.value == "asset:${asset.id}" && bindObjectTarget(asset.target)) navigate("asset-edit:${asset.id}")
    }

    fun closeAssetEditor(id: String, more: Boolean = false) {
        if (targetForAssetEditor(id) == null) return
        back()
        if (route.value != "asset:$id") return
        if (more) saved["pendingAssetTools"] = id
        refreshAsset(id)
    }

    fun consumeAssetTools(id: String) {
        if (pendingAssetTools.value == id) saved["pendingAssetTools"] = null
    }

    fun openAsset(id: String, sourceKey: String? = null, target: Session? = session.value, seed: Asset? = null) {
        if (target == null || !bindObjectTarget(target)) return
        if (route.value != "asset:$id" || saved.get<String>("viewAsset") != id) {
            saved["viewSession"] = UUID.randomUUID().toString()
            saved["viewAsset"] = id
        }
        if (sourceKey != null) saved["detailListKey"] = sourceKey
        navigate("asset:$id")
        // 列表项已带本资产的身份和地址时先显示，详情请求随后静默覆盖；旧版同样是先出图再补数据。
        if (seed != null && seed.id == id && sameTarget(seed.target, target)) {
            mutableAsset.value = seed
        } else if (mutableAsset.value?.id != id) {
            mutableAsset.value = null
        }
        mutableAssetAuthors.value = emptyList()
        detailJob?.cancel()
        val ticket = detailGeneration.next()
        val connection = connectionGeneration
        detailJob = viewModelScope.launch {
            try {
                val result = repository.asset(id, target)
                if (!detailGeneration.isCurrent(ticket) || connection != connectionGeneration || !sameTarget(target, session.value)) return@launch
                mutableAsset.value = result
                val names = coroutineScope { result.authorIds.map { async { repository.author(it, target) } }.awaitAll() }
                if (detailGeneration.isCurrent(ticket) && connection == connectionGeneration && sameTarget(target, session.value)) mutableAssetAuthors.value = names
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (!detailGeneration.isCurrent(ticket) || !sameTarget(target, session.value)) return@launch
                // 旧版口径：屏幕上已有这一件（种子上屏或上一次结果）时数据刷新失败保持静默，
                // 画面不动；只有无内容可显示时才需要错误出口。HTTP 业务失败保持原提示。
                val displayed = mutableAsset.value?.takeIf { it.id == id }
                if (detailFailureIsSilent(failure, displayed != null)) return@launch
                showFailure(failure)
            }
        }
    }

    fun adjacentAsset(direction: Int) {
        val id = mutableAsset.value?.id ?: return
        val source = saved.get<String>("detailListKey")?.let { lists[it]?.value?.items }.orEmpty()
        val index = source.indexOfFirst { it.id == id }
        // index=-1 表示当前件不在这份已加载清单里：不能再做 index+direction 后取 0 号位，
        // 否则左滑会「穿」到列表第一张（旧版按批次内 id 定位，不在批次内就不切）。
        val next = adjacentAssetIndex(index, direction, source.size)?.let(source::get) ?: return
        // A sibling replaces the detail scene, without growing its return stack.
        saved["route"] = "asset:${next.id}"
        openAsset(next.id, target = next.target, seed = next)
    }

    fun detailPosition(id: String): Pair<Int, Int> {
        val batch = saved.get<String>("detailListKey")?.let { lists[it]?.value?.items }.orEmpty()
        return batch.indexOfFirst { it.id == id } to batch.size
    }

    /**
     * 详情邻位原图候选：只取已加载清单内的邻居，不为了预载去多拉批次。
     * 旧版 DetailPreloadPolicy 取前 2 后 1；是否值得预载（字节上限、媒体类型）由调用方按
     * 列表行自带的事实判断，这里只负责位置。
     */
    fun detailNeighbourOriginals(id: String, back: Int, forward: Int): List<Asset> {
        val source = saved.get<String>("detailListKey")?.let { lists[it]?.value?.items }.orEmpty()
        val index = source.indexOfFirst { it.id == id }
        if (index < 0) return emptyList()
        val ahead = ((index + 1)..minOf(index + forward, source.lastIndex)).map(source::get)
        val behind = ((index - 1) downTo maxOf(0, index - back)).map(source::get)
        return ahead + behind
    }

    fun setFavorite(asset: Asset, favorite: Boolean) = applyReaction("favorite", asset, favorite) { key -> repository.setFavorite(asset, favorite, key) }
    fun setTodayLike(asset: Asset, liked: Boolean) = applyReaction("like", asset, liked) { key -> repository.setTodayLike(asset, liked, key) }

    private fun applyReaction(field: String, asset: Asset, enabled: Boolean, send: suspend (String) -> Asset) = action {
        if (!sameTarget(asset.target, session.value)) return@action
        val intent = ReactionIntent(asset.target, asset.id, asset.revision, enabled)
        val key = reactionRetries[field]?.takeIf { it.first == intent }?.second ?: UUID.randomUUID().toString()
        // An ambiguous response leaves the key attached to this exact gesture on the displayed revision.
        reactionRetries[field] = intent to key
        val updated = try { send(key) } catch (failure: ApiFailure) {
            if (failure.status in 400..499 && failure.status !in setOf(408, 429) && reactionRetries[field]?.second == key) reactionRetries.remove(field)
            throw failure
        }
        if (reactionRetries[field]?.second == key) reactionRetries.remove(field)
        publishAsset(updated)
        if (!sameTarget(updated.target, session.value)) return@action
        queries.filterValues { it.favorite != null || it.liked != null }.keys.forEach { name ->
            cancelBrowse(name)
            lists[name]?.let { it.value = it.value.copy(loaded = false) }
        }
    }
    fun saveTags(tags: List<String>, expectedRevision: Long, onSaved: () -> Unit) = action {
        val current = mutableAsset.value ?: return@action
        publishAsset(repository.tags(current.copy(tagsRevision = expectedRevision), tags)); onSaved()
    }
    fun saveNote(asset: Asset, note: String, onSaved: () -> Unit) = action {
        publishAsset(repository.updateNote(asset, note)); onSaved()
    }
    fun saveRelations(asset: Asset, sources: List<String>, characters: List<String>, onSaved: () -> Unit) = action {
        publishAsset(repository.updateRelations(asset, sources, characters)); onSaved()
    }
    fun saveAcquisitionSources(asset: Asset, sources: List<String>, onSaved: () -> Unit) = action {
        publishAsset(repository.updateAcquisitionSources(asset, sources)); onSaved()
    }
    fun loadTags(next: Boolean = false) = action(blocking = false) { readTags(next) }

    private suspend fun readTags(next: Boolean) {
        val cursor = if (next) (mutableTagCursor.value ?: return) else null
        val ticket = tagReads.next()
        val page = repository.tags(cursor)
        if (!tagReads.isCurrent(ticket)) return
        mutableTags.value = if (next) (mutableTags.value + page.items).distinct() else page.items
        mutableTagCursor.value = page.nextCursor
    }
    fun removeTag(tag: String, onSaved: (Asset) -> Unit) = action {
        mutableAsset.value?.let { val result = repository.removeTag(it, tag); publishAsset(result); onSaved(result) }
    }
    fun createTag(name: String, onSaved: (String) -> Unit) = action { onSaved(repository.createTag(name)) }
    fun refreshTagsState() = action {
        mutableAsset.value?.let { publishAsset(repository.asset(it.id, it.target)) }
        readTags(false)
    }
    fun viewSessionId(): String = saved.get<String>("viewSession") ?: UUID.randomUUID().toString().also { saved["viewSession"] = it }
    fun recordView(asset: Asset, sessionId: String) = graph.recordEvent(asset, "view", sessionId)
    fun recordProgress(asset: Asset, position: Long, sessionId: String) = graph.recordEvent(asset, "progress", sessionId, positionMs = position)
    fun recordPlay(asset: Asset, sessionId: String) = graph.recordEvent(asset, "play", sessionId)
    fun recordDwell(asset: Asset, sessionId: String, seconds: Double, startedAt: String) {
        if (seconds >= 1.0) graph.recordEvent(asset, "dwell", sessionId, seconds = seconds, occurredAt = startedAt)
    }
    fun mediaClient(target: Session) = repository.mediaClient(target)
    fun addThumbnailImageComponents(builder: coil3.ComponentRegistry.Builder) = graph.thumbnails.addImageComponents(builder)
    private fun publishAsset(asset: Asset) {
        if (!sameTarget(asset.target, session.value)) return
        if (mutableAsset.value?.let { it.id == asset.id && it.revision <= asset.revision } == true) mutableAsset.value = asset
        // 收藏/点赞/标签等保存只改一条资产：不含该 id 的列表整张跳过，避免每次保存都复制所有已加载列表。
        lists.values.forEach { state ->
            val current = state.value
            if (current.items.none { it.id == asset.id }) return@forEach
            var changed = false
            val items = current.items.map { item ->
                if (item.id == asset.id && item.revision <= asset.revision) {
                    changed = true
                    asset.copy(rankingScore = item.rankingScore, lastViewedAt = item.lastViewedAt)
                } else item
            }
            if (changed) state.value = current.copy(items = items)
        }
    }

    fun loadAuthors(next: Boolean = false) = loadPage(authorState, next) { repository.authors(it) }
    fun loadReports(next: Boolean = false) = loadPage(reportState, next) { repository.reports(it) }
    fun loadAuthorTasks(next: Boolean = false) = loadPage(authorTaskState, next) { repository.authorTasks(it) }
    fun cancelAuthorTask(id: String) = action { repository.cancelAuthorTask(id); loadAuthorTasks() }
    /**
     * 进入作者作品页。
     *
     * [seed] = 调用方点按的那一项（作者管理行 / 详情作者卡）已经持有的作者对象。旧版把
     * authorName 放进路由参数（feature/author/AuthorCollectionRoutes.kt:19,30），标题第一帧
     * 就是真名；新版把同样的信息用 seed 带过来，名字与 fileCount 立即上屏，详情请求随后
     * 静默覆盖。同一作者再次打开时保留现值而不是先清空（旧实现在每次 open 时写 null，
     * 造成标题回落「作者」、计数回落「—」）。
     */
    fun openAuthor(id: String, seed: Author? = null) {
        navigate("author:$id")
        val known = seed?.takeIf { it.id == id } ?: mutableAuthor.value?.takeIf { it.id == id }
        if (mutableAuthor.value != known) mutableAuthor.value = known
        action { val result = repository.author(id); if (route.value == "author:$id") mutableAuthor.value = result }
    }
    fun followAuthor(authorId: String) {
        val current = mutableAuthor.value?.takeIf { it.id == authorId } ?: return
        val target = session.value ?: return
        action {
            mutableAuthorFollow.value = LoadState(data = authorId, loading = true)
            try {
                val updated = repository.follow(current, target)
                if (sameTarget(target, session.value) && mutableAuthor.value?.id == authorId) mutableAuthor.value = updated
                if (sameTarget(target, session.value)) mutableAuthorFollow.value = LoadState()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (failure: Exception) {
                if (sameTarget(target, session.value)) mutableAuthorFollow.value = LoadState(data = authorId, error = explain(failure))
            }
        }
    }
    fun followAssetAuthor(author: Author) = action {
        val updated = repository.follow(author)
        mutableAssetAuthors.value = mutableAssetAuthors.value.map { if (it.id == author.id) updated else it }
        if (mutableAuthor.value?.id == author.id) mutableAuthor.value = updated
    }
    fun editAuthor(name: String, aliases: List<String>, biography: String, homepage: String, onSaved: () -> Unit) = action {
        mutableAuthor.value?.let { mutableAuthor.value = repository.editAuthor(it, name, aliases, biography, homepage); onSaved() }
    }
    fun createAuthor(name: String, onSaved: () -> Unit) = action { repository.createAuthor(name); onSaved(); loadAuthors() }
    fun deleteCurrentAuthor() = action {
        mutableAuthor.value?.let { repository.deleteAuthor(it); back(); loadAuthors() }
    }
    fun mergeCurrentAuthor(target: Author, onSaved: () -> Unit) = action {
        mutableAuthor.value?.let {
            val result = repository.mergeAuthors(it, target)
            mutableAuthor.value = result.first
            saved["route"] = "author:${result.first.id}"
            publishMessage(result.second)
            onSaved(); loadAuthors()
        }
    }
    fun createDirectory(libraryId: String, parent: String, name: String, onSaved: () -> Unit) = action {
        require(name.isNotBlank() && '/' !in name && '\\' !in name) { "请输入单个目录名称" }
        repository.createDirectory(libraryId, listOf(parent, name.trim()).filter(String::isNotEmpty).joinToString("/"))
        onSaved()
    }
    fun trashCurrentAsset(asset: Asset) = action {
        repository.trash(asset)
        lists.values.forEach { it.value = it.value.copy(loaded = false, items = it.value.items.filterNot { item -> item.id == asset.id }) }
        if (route.value == "asset:${asset.id}") back()
    }
    fun renameCurrentAsset(asset: Asset, path: String, onSaved: () -> Unit) = action {
        publishAsset(repository.rename(asset, path)); onSaved()
    }
    fun loadTrash(next: Boolean = false) = loadPage(trashState, next) { repository.trashPage(it) }
    fun restoreAsset(asset: Asset) = action {
        repository.restore(asset)
        trashState.value = trashState.value.copy(items = trashState.value.items.filterNot { it.id == asset.id })
        lists.values.forEach { it.value = it.value.copy(loaded = false) }
    }
    fun purgeAsset(asset: Asset) = action {
        repository.purge(asset)
        trashState.value = trashState.value.copy(items = trashState.value.items.filterNot { it.id == asset.id })
    }
    fun refreshReport(id: String) { targetForRefresh("task:$id")?.let { openReport(id, it) } }

    fun openReport(id: String, target: Session? = session.value) {
        if (target == null || !bindObjectTarget(target)) return
        navigate("task:$id"); mutableReport.value = null
        action {
            val result = repository.report(id, target)
            if (route.value == "task:$id" && sameTarget(target, session.value)) mutableReport.value = result
        }
    }
    fun cancelReport() = action { mutableReport.value?.let { mutableReport.value = repository.cancelReport(it) } }
    fun loadStatistics() = action { mutableStats.value = repository.statistics() }
    /** 最近一次统计请求的范围；只在该请求的 job 仍存活时参与判定（见 [ensureAnalytics]）。 */
    private var analyticsRange: String? = null
    /** 当前报告落地的单调毫秒；只用于新鲜度窗口判定（见 [ANALYTICS_FRESHNESS_MS]）。 */
    private var analyticsLoadedAtMs: Long? = null
    /**
     * 统计页三档重新进入时保留上一份报告：旧版统计卡片的数字在重查期间不消失，只有
     * 从未取到过的范围才显示加载文案。成功整份替换、失败保留旧数字并在页内提示。
     */
    fun loadAnalytics(range: String) {
        analyticsJob?.cancel()
        val previous = analyticsState.value
        analyticsState.value = previous.copy(loading = true, error = null)
        val connection = connectionGeneration
        analyticsRange = range
        analyticsJob = viewModelScope.launch {
            try {
                val result = graph.analytics.report(range)
                if (connection == connectionGeneration) {
                    analyticsState.value = LoadState(data = result)
                    analyticsLoadedAtMs = monotonicNowMs()
                }
            }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (connection == connectionGeneration) analyticsState.value = previous.copy(loading = false, error = explain(failure)) }
        }
    }

    /**
     * 统计页与统计详情进入（或范围变化、回前台）时的惰性入口：本范围报告仍在新鲜窗口内、
     * 或本范围请求已在途中时不重复请求；范围变化、从未加载、窗口过期、上次失败仍走
     * [loadAnalytics] 立即重取（旧报告保留在屏上，见 [loadAnalytics]）。
     * 错误卡与显式刷新继续直接调 [loadAnalytics] 强制请求（不受本闸门约束）。
     */
    fun ensureAnalytics(range: String) {
        val inFlightRange = if (analyticsJob?.isActive == true) analyticsRange else null
        if (shouldLoadAnalytics(analyticsState.value.data?.overview?.range, analyticsLoadedAtMs,
                analyticsState.value.error, inFlightRange, range, monotonicNowMs())) loadAnalytics(range)
    }
    fun loadReview(range: String) {
        reviewJob?.cancel()
        val previous = reviewState.value
        reviewState.value = previous.copy(loading = true, error = null)
        val connection = connectionGeneration
        reviewJob = viewModelScope.launch {
            try { val result = graph.analytics.review(range); if (connection == connectionGeneration) reviewState.value = LoadState(data = result) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (connection == connectionGeneration) reviewState.value = previous.copy(loading = false, error = explain(failure)) }
        }
    }
    fun loadRecommendationPreferences() = action { recommendationState.value = graph.analytics.preferences() }
    fun loadRanking(kind: String, range: String, metric: String, next: Boolean = false) {
        val query = "$kind:$range:$metric"
        if (next && (rankingState.value.loading || rankingState.value.cursor == null || rankingQuery != query)) return
        rankingJob?.cancel()
        val previous = if (rankingQuery == query) rankingState.value else ListState<ActivityRank>()
        rankingQuery = query
        rankingState.value = previous.copy(loading = true, error = null)
        val connection = connectionGeneration
        rankingJob = viewModelScope.launch {
            try {
                val result = graph.analytics.ranking(kind, range, metric, if (next) previous.cursor else null)
                if (rankingQuery == query && connection == connectionGeneration)
                    rankingState.value = ListState(if (next) previous.items + result.items else result.items, result.nextCursor, loaded = true)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (rankingQuery == query && connection == connectionGeneration) rankingState.value = previous.copy(error = explain(failure)) }
        }
    }
    fun chooseRecommendationPreset(preset: RecommendationWeights) = action {
        recommendationState.value?.let { recommendationState.value = graph.analytics.savePreferences(it, preset) }
        lists.filterKeys { it.startsWith("home:") }.values.forEach { it.value = it.value.copy(loaded = false) }
    }

    fun setUploadLibrary(id: String) = action(blocking = false) { persistUploadLibrary(id) }
    private suspend fun persistUploadLibrary(id: String) {
        session.value?.let { target -> withContext(Dispatchers.IO) { graph.settings.saveUploadLibrary(target, id) } }
        if (id != uploadLibrary.value) {
            saved["uploadAuthorIds"] = emptyList<String>(); saved["uploadCollectionId"] = null; saved["uploadSources"] = ""; saved["uploadFamily"] = ""; saved["uploadAutoName"] = false
            saved["uploadDirectory"] = ""; saved["uploadAuthorSources"] = ""
            collectionJob?.cancel(); collectionState.value = ListState()
        }
        saved["uploadLibrary"] = id
    }
    fun setUploadDirectory(directory: String) { saved["uploadDirectory"] = directory }
    fun setUploadMode(mode: UploadMode) { saved["uploadMode"] = mode.name }
    fun setUploadAuthors(ids: List<String>) {
        if (ids != uploadAuthorIds.value) saved["uploadAuthorSources"] = ""
        saved["uploadAuthorIds"] = ids
    }
    fun setUploadCollection(id: String?) { saved["uploadCollectionId"] = id }
    fun setUploadNote(note: String) { saved["uploadNote"] = note }
    fun setUploadSources(sources: String) { saved["uploadSources"] = sources }
    fun setUploadAuthorSources(sources: String) { saved["uploadAuthorSources"] = sources }
    fun setUploadFamily(family: String) { saved["uploadFamily"] = family }
    fun setUploadAutoName(enabled: Boolean) { saved["uploadAutoName"] = enabled }
    fun loadCollections(next: Boolean = false) {
        if (next && (collectionState.value.loading || collectionState.value.cursor == null)) return
        collectionJob?.cancel()
        val library = uploadLibrary.value
        val previous = collectionState.value.copy(loading = false)
        val connection = connectionGeneration
        collectionState.value = previous.copy(loading = true, error = null)
        collectionJob = viewModelScope.launch {
            try {
                val page = repository.collections(library, if (next) previous.cursor else null)
                if (library == uploadLibrary.value && connection == connectionGeneration)
                    collectionState.value = ListState(if (next) previous.items + page.items else page.items, page.nextCursor, loaded = true)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (library == uploadLibrary.value && connection == connectionGeneration) collectionState.value = previous.copy(error = explain(failure))
            }
        }
    }
    fun createCollection(name: String, kind: String, onSaved: () -> Unit) = action {
        repository.createCollection(uploadLibrary.value, name, kind); onSaved(); loadCollections()
    }
    fun receiveFiles(uris: List<Uri>, shared: Boolean = false) {
        navigate("upload")
        val target = session.value
        val destination = if (shared && target != null) graph.settings.uploadLibrary(target) else null
        val directory = if (destination == uploadLibrary.value && saved.get<String>("uploadInstance") == target?.instanceId) uploadDirectory.value else ""
        // Transient Android URI grants must be consumed even when another UI action is busy or a login changes.
        viewModelScope.launch {
            try {
                graph.awaitRecovery()
                val group = if (target != null && !destination.isNullOrEmpty()) "share-${UUID.randomUUID()}" else null
                try {
                    val inputs = graph.uploads.receive(uris, intakeGroup = group)
                    if (group == null) return@launch
                    if (target != null && destination != null && sameTarget(target, session.value) && repository.library(target, destination).enabled) {
                        graph.uploads.enqueue(destination, directory, UploadMode.ORDINARY, target = target, inputIds = inputs, batchKey = group)
                    } else publishMessage("原上传位置已改变，本次分享已保留，请选择上传位置")
                } finally {
                    if (group != null) withContext(NonCancellable) { graph.uploads.releaseSharedDrafts(group) }
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { showFailure(failure) }
        }
    }
    fun startUpload() = action {
        val target = session.value ?: error("请先连接服务器")
        val libraryId = uploadLibrary.value
        val directory = uploadDirectory.value
        val mode = UploadMode.valueOf(uploadMode.value)
        val snapshot = queue.value ?: error("请先恢复传输记录")
        val stems = editedStems.value.toMap()
        val draftFiles = snapshot.drafts.filter { it.intakeGroup == null }.map { draft ->
            val stem = stems[draft.id]
            require(stem == null || draft.validStem(stem)) { "请输入有效的作品名" }
            draft.withEditedStem(stem)
        }
        val inputs = snapshot.pendingInputs.filter { it.intakeGroup == null }.map { it.id }
        check(draftFiles.all { it.id in inputs }) { "旧待传项缺少原始输入身份，请重新选择" }
        check(libraries.value.any { it.id == libraryId && it.enabled }) { "请选择正在接收上传的影库" }
        val family = uploadFamily.value.trim().takeIf { mode == UploadMode.ORDINARY && (uploadAutoName.value || uploadCollectionId.value != null) }?.takeIf(String::isNotBlank)
        val options = UploadOptions(uploadAuthorIds.value, uploadCollectionId.value, uploadNote.value, uploadSources.value.lines().filter(String::isNotBlank), family,
            if (uploadAuthorIds.value.isEmpty()) emptyList() else uploadAuthorSources.value.lines().map(String::trim).filter(String::isNotEmpty))
        val namingDrafts = draftFiles.filter { it.bytes > 0 }
        if (mode == UploadMode.ORDINARY && (uploadAutoName.value || options.collectionId != null) && namingDrafts.isNotEmpty()) {
            val inspection = graph.naming.inspect(NamingRequest(libraryId, directory, family.orEmpty(), options.collectionId,
                if (namingDrafts.all { it.mimeType.startsWith("video/") }) "video" else "image",
                options.note, namingDrafts.map { NamingFile(it.id, it.name) }))
            check(inspection.canSubmit) { "请先填写作品基名并选择文件" }
        }
        check(sameTarget(target, session.value)) { "服务器已改变，请重新确认上传位置" }
        graph.uploads.enqueue(libraryId, directory, mode, options, target, inputs, reviewed = draftFiles, editedStems = stems)
        saved["uploadStems"] = HashMap(editedStems.value.filterKeys { it !in inputs })
    }
    fun editStem(draft: UploadDraft, stem: String) {
        saved["uploadStems"] = HashMap(editedStems.value).apply { put(draft.id, stem) }
        nameWrites.remove(draft.id)?.cancel()
        if (!draft.validStem(stem)) return
        nameWrites[draft.id] = viewModelScope.launch {
            try { delay(350); graph.uploads.renameDraft(draft.id, draft.withEditedStem(stem).name) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { showFailure(failure) }
            finally { if (nameWrites[draft.id] == coroutineContext[Job]) nameWrites.remove(draft.id) }
        }
    }
    fun removeDraft(id: String) = action {
        nameWrites.remove(id)?.cancel()
        graph.uploads.discardDraft(id)
        saved["uploadStems"] = HashMap(editedStems.value).apply { remove(id) }
    }
    fun retryUpload(id: String) = action { graph.uploads.retry(id) }
    fun cancelUpload(id: String) = action { graph.uploads.cancel(id) }
    fun clearUploads() = action {
        val previously = queue.value?.transfers.orEmpty().filter { it.archiveSourceState != null }.mapTo(mutableSetOf(), Transfer::id)
        graph.uploads.clearFinished()
        graph.archives.prune(previously - queue.value?.transfers.orEmpty().map(Transfer::id).toSet())
    }
    fun retryArchiveSource(task: Transfer) = action {
        graph.archives.retryProtection(task)
        graph.queue.update { data -> data.copy(transfers = data.transfers.map {
            if (it.id == task.id) it.copy(archiveSourceState = "pending", message = "已入库；正在重新核对本批独立备份") else it
        }) }
        graph.uploads.schedule()
    }
    fun reloadQueue() = action { graph.queue.load(); graph.uploads.schedule() }
    fun retryEvents(target: Session, id: String? = null) = action { graph.activityEvents.retry(target, id) }
    fun dismissEvent(target: Session, event: PendingEvent) = action { graph.activityEvents.dismiss(target, event) }
    fun savePreferences(preferences: Preferences) = action { withContext(Dispatchers.IO) { graph.settings.savePreferences(preferences) } }

    private fun <T> loadPage(state: MutableStateFlow<ListState<T>>, next: Boolean, loader: suspend (String?) -> Page<T>) {
        if (next && (state.value.loading || state.value.cursor == null)) return
        pageJobs.remove(state)?.cancel()
        val old = state.value.copy(loading = false)
        val generation = connectionGeneration
        state.value = old.copy(loading = true, error = null)
        pageJobs[state] = viewModelScope.launch {
            try {
                val result = loader(if (next) old.cursor else null)
                if (generation == connectionGeneration) state.value = ListState(if (next) old.items + result.items else result.items, result.nextCursor,
                    loaded = true, totalMatched = result.totalMatched, dateCounts = if (next) old.dateCounts else result.dateCounts)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { if (generation == connectionGeneration) state.value = old.copy(error = explain(failure)) }
        }
    }

    private fun action(blocking: Boolean = true, surfaceConnectionFailure: Boolean = true, operation: suspend () -> Unit) {
        if (blocking && mutableBusy.value) return
        val generation = connectionGeneration
        if (blocking) mutableBusy.value = true
        if (blocking) clearMessageNow()
        val job = viewModelScope.launch(start = CoroutineStart.LAZY) {
            try { operation() }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (generation == connectionGeneration) {
                    // 自动恢复/后台维护传 false：传输层失败本就没有用户动作可归因，由页面态呈现。
                    // 非连接类失败（凭据失效、协议/业务校验）仍然必须让用户看到，不能被选项吞掉。
                    if (surfaceConnectionFailure || !isTransportFailure(failure)) showFailure(failure)
                }
            }
            finally { if (blocking && generation == connectionGeneration) mutableBusy.value = false }
        }
        activeOperations += job
        job.invokeOnCompletion { activeOperations.remove(job) }
        job.start()
    }
}

/**
 * 同一范围报告的新鲜窗口（毫秒）：窗口内重进统计页不重取五组接口；回前台或再次进入时
 * 超过窗口就在后台补读，旧数字继续留在屏上直到新报告落地。与旧版「每次恢复都重查」相比
 * 只是省掉重复抖动，不改变数据最终会重新对齐的语义。
 */
internal const val ANALYTICS_FRESHNESS_MS = 60_000L

/**
 * 统计页是否要发起新请求（纯判断，单测锁定；由 [LibraryViewModel.ensureAnalytics] 使用）。
 *
 * - [inFlightRange] 与 [requestedRange] 相同：本范围已在请求中，重复进入不取消重启。
 * - [inFlightRange] 是别的范围：必须改取当前范围。渲染按 `overview.range == requestedRange`
 *   过滤报告，若放任旧范围的请求落地，页面会停在「加载中」而没有请求去补当前范围。
 * - [loadedRange] 等于 [requestedRange]、无 [error]，且报告在 [ANALYTICS_FRESHNESS_MS]
 *   内落地：不重取、不闪卡片；窗口过期则重取。
 * - 其余（从未加载、范围不同、窗口过期、上次失败）：立即请求；错误卡另由调用方直接
 *   [LibraryViewModel.loadAnalytics] 强制刷新。
 */
internal fun shouldLoadAnalytics(
    loadedRange: String?,
    loadedAtMs: Long?,
    error: String?,
    inFlightRange: String?,
    requestedRange: String,
    nowMs: Long,
): Boolean = when {
    inFlightRange == requestedRange -> false
    inFlightRange != null -> true
    error != null -> true
    loadedRange == requestedRange && loadedAtMs != null && nowMs - loadedAtMs < ANALYTICS_FRESHNESS_MS -> false
    else -> true
}

/** 单调时钟（毫秒，含休眠）：新鲜度只用差值判断，不受系统时间调整影响。 */
private fun monotonicNowMs(): Long = SystemClock.elapsedRealtime()

/**
 * 服务器事件计数与会话的配对闸门。
 *
 * 事件流每次（重新）建立都会把所有计数各加一次（`ServerEventSignals.streamOpened`），
 * 因为断线期间的事件无法重放，重连后补一次权威读取是正确的补偿。但在登录 / 换服 / 登出
 * 引起的会话切换上，这一次补增不能触发「重读已加载列表」：那一刻列表状态仍属于旧会话，
 * 拿旧查询去 browse 会打新服务器（或登出后根本没有会话）。因此：
 * - 没有会话时不接受；
 * - 与上一次计数属于同一会话才接受（会话刚出现/刚更换的第一次计数只当作重连）。
 * 纯逻辑，单测锁定。
 */
internal class ServerEventSessionGate {
    private var lastSession: Session? = null
    private var seen = false

    /** 会话切换（登录 / 登出 / 换服）：下一次计数一律只算重连，不补读上一会话的列表。 */
    fun forget() {
        lastSession = null
        seen = false
    }

    /** 记录本次计数对应的会话；true 表示同一会话上的真实推送，可以补读。 */
    fun accept(current: Session?): Boolean {
        if (current == null) {
            lastSession = null
            return false
        }
        val sameSession = seen && current == lastSession
        lastSession = current
        seen = true
        return sameSession
    }
}

/**
 * 连续空页上限（值 3 沿用旧版 HomeViewModel.MAX_EMPTY_SEED_ROUNDS = 3 的同款裁决：有限库
 * 深翻页被掏干后，3 轮空转即可判定穷尽，多试只是白打请求）。这里是「页」而不是「换 seed
 * 轮次」：服务端逐页过滤已回收/停用候选时会返回 0 条 + 非空游标，若不设上限，触底哨兵会
 * 在 0ms 内链式要下一页，批量回收/停用后一次滑动就能打出成串请求。达到上限后把游标视为
 * 到底（`cursor = null`），列表停手；下拉刷新或换查询重新计数。
 */
private const val MAX_EMPTY_APPEND_PAGES = 3

/**
 * 下拉刷新成功但内容与刷新前完全一致的一次性提示（旧版 ListQueryDefaults.LIST_UP_TO_DATE_MESSAGE
 * 同文案）。服务端返回同页同序数据时界面零变化，用户无法区分「没反应」与「刷新了但没新的」。
 */
internal const val LIST_UP_TO_DATE_MESSAGE = "已是最新，内容没有变化"

/** 轻提示自动消退时间（毫秒）：旧版 HomeViewModel.INFO_HINT_AUTO_CLEAR_MS = 2_500L 同值。 */
private const val UP_TO_DATE_HINT_MS = 2_500L

/**
 * 翻页（append）网络失败自动重试延迟（毫秒）：旧版 ListQueryDefaults.APPEND_RETRY_DELAYS_MS
 * = listOf(1_000L, 3_000L) 同值（2026-09-28 问题A「翻页失败即卡死」）。第 1 次失败后等 1 秒、
 * 再失败等 3 秒，仍失败才亮错误横幅；两次封顶防持续故障下无限锤击请求。
 */
internal val APPEND_RETRY_DELAYS_MS = listOf(1_000L, 3_000L)

/**
 * 第 [attempt] 次失败（从 0 计）后的重试延迟；null = 不重试。只有翻页（[append]）重试：
 * 首载/显式刷新/筛选变化是用户显式动作，失败要立即反馈；翻页失败时用户往往钉在底部等待，
 * 不重试就必须上滚再回滚才能再次触发。纯函数，重试节奏由单测锁定。
 */
internal fun appendRetryDelayMs(append: Boolean, attempt: Int): Long? =
    if (append) APPEND_RETRY_DELAYS_MS.getOrNull(attempt) else null

/**
 * [LibraryViewModel.adjacentAsset] 的目标下标（纯函数，单测锁定）。
 *
 * 当前件不在已加载清单时 [index] = -1：若照旧做 `index + direction` 再 `getOrNull`，
 * 左滑（direction=+1，下一张）会落到 0 号位，用户看到详情「穿」到列表第一张而不是相邻件。
 * 因此 index 不在 [0, size) 时直接返回 null；目标越过清单首/尾（第一张右滑、最后一张
 * 左滑）同样返回 null，保持原有边界行为。
 */
internal fun adjacentAssetIndex(index: Int, direction: Int, size: Int): Int? {
    if (index < 0 || index >= size) return null
    val target = index + direction
    return target.takeIf { it in 0 until size }
}

/** 屏幕上现有的清单与刷新返回的首屏逐项同 id、同顺序、同长度（不把重排冒充变化）。 */
private fun sameAssetIds(onScreen: List<Asset>, fresh: List<Asset>): Boolean =
    onScreen.size == fresh.size && onScreen.indices.all { onScreen[it].id == fresh[it].id }

/**
 * 分页追加：一趟扫描按 id 去重（先出现的保留）。原实现 `old + page` 之后 `distinctBy` 要分配
 * 两份整表；这里只分配结果表与一个 id 集合，顺序与去重语义不变。
 */
private fun appendDistinct(existing: List<Asset>, appended: List<Asset>): List<Asset> {
    val seen = HashSet<String>(existing.size + appended.size)
    val merged = ArrayList<Asset>(existing.size + appended.size)
    for (asset in existing) if (seen.add(asset.id)) merged.add(asset)
    for (asset in appended) if (seen.add(asset.id)) merged.add(asset)
    return merged
}

/**
 * 传输层失败判定（纯函数，单测锁定）：连接拒绝、超时、断流都是 IO 异常；
 * [ApiFailure] 虽然也继承 IOException，但它代表「已经收到 HTTP 应答」（401/403/404/409/5xx
 * 都有明确业务含义），不能当作连接问题静默或重试。
 */
internal fun isTransportFailure(failure: Throwable): Boolean = failure is java.io.IOException && failure !is ApiFailure

/**
 * 详情数据刷新失败是否静默（纯函数，单测锁定）。
 *
 * 屏幕上已经显示当前资产（列表种子上屏，或上一次详情结果还在）时，传输失败保持画面、
 * 不弹全局提示——旧版详情就是这样：种子继续显示，失败只影响「补数据」这一步。
 * 没有内容可显示时仍需要错误出口；非传输失败（HTTP 业务应答、解析/校验异常）也保持
 * 原有提示，用户需要知道对象已不存在、没有权限或数据有问题。
 */
internal fun detailFailureIsSilent(failure: Throwable, assetDisplayed: Boolean): Boolean =
    assetDisplayed && isTransportFailure(failure)

/** 连接失败提示的文本前缀：只有这类文本参与冷却去重，业务错误每次都要照常提示。 */
internal const val CONNECTION_MESSAGE_PREFIX = "连接失败"

/** 同一连接失败文本的冷却窗口（毫秒）：几秒内的连续失败只保留一条横幅。 */
internal const val CONNECTION_MESSAGE_COOLDOWN_MS = 4_000L

/**
 * 连接失败提示的去重判定（纯函数，单测锁定）：
 * - 非连接类文本永远照常发布（业务校验、权限、冲突等不能被冷却吞掉）；
 * - 同一文本已经在屏上：保留现有实例，不重发（否则横幅会闪一下再出现）；
 * - 同一文本在冷却窗内刚发布过：保留，不重发；
 * - 其他情况：允许发布。
 */
internal fun shouldPostConnectionMessage(
    text: String,
    visible: String?,
    lastPostedText: String?,
    lastPostedAtMs: Long?,
    nowMs: Long,
    cooldownMs: Long = CONNECTION_MESSAGE_COOLDOWN_MS,
): Boolean {
    if (!text.startsWith(CONNECTION_MESSAGE_PREFIX)) return true
    if (visible == text) return false
    if (lastPostedText != text || lastPostedAtMs == null) return true
    return nowMs - lastPostedAtMs >= cooldownMs
}

/** 冷却状态持有者：发布连接失败时记录，任何成功/清除路径调用 [reset]。 */
internal class ConnectionMessageGate(private val cooldownMs: Long = CONNECTION_MESSAGE_COOLDOWN_MS) {
    private var lastPostedText: String? = null
    private var lastPostedAtMs: Long? = null

    fun shouldPost(text: String, visible: String?, nowMs: Long): Boolean {
        val post = shouldPostConnectionMessage(text, visible, lastPostedText, lastPostedAtMs, nowMs, cooldownMs)
        if (post && text.startsWith(CONNECTION_MESSAGE_PREFIX)) {
            lastPostedText = text
            lastPostedAtMs = nowMs
        }
        return post
    }

    fun reset() {
        lastPostedText = null
        lastPostedAtMs = null
    }
}

private fun explain(failure: Exception): String = when (failure) {
    is ApiFailure -> failure.message
    is java.io.IOException -> "连接失败，请检查服务器后重试"
    is IllegalArgumentException, is IllegalStateException -> failure.message ?: "操作未完成"
    else -> "操作未完成，请重试"
}
