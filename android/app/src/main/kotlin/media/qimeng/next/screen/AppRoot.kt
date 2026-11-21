package media.qimeng.next.screen

import android.animation.ValueAnimator
import android.os.Build
import androidx.activity.compose.BackHandler
import androidx.activity.compose.LocalActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.core.view.WindowCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil3.ImageLoader
import coil3.annotation.ExperimentalCoilApi
import coil3.gif.AnimatedImageDecoder
import coil3.gif.GifDecoder
import coil3.memory.MemoryCache
import coil3.network.ConnectivityChecker
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.AssetEditorTarget
import media.qimeng.next.model.Preferences
import media.qimeng.next.model.Session
import media.qimeng.next.model.ThemeChoice
import media.qimeng.next.ui.*
import media.qimeng.next.ui.glass.GlassNavItem
import media.qimeng.next.ui.glass.rememberQimengBackdropState
import media.qimeng.next.ui.glass.qimengBackdropSource
import media.qimeng.next.ui.icon.HomeIcon
import media.qimeng.next.ui.icon.AlbumIcon
import media.qimeng.next.ui.icon.StatsIcon
import media.qimeng.next.ui.icon.ProfileIcon

val LocalMediaImages = staticCompositionLocalOf<ImageLoader> { error("ImageLoader missing") }

/** Coil 内存缓存占最大堆比例：旧版 CoilModule 同值 25%（Coil 3.1 默认 20%/低内存 15%）。 */
private const val MEMORY_CACHE_PERCENT = 0.25

@OptIn(ExperimentalCoilApi::class)
@Composable
fun AppRoot(model: LibraryViewModel) {
    val preferences by model.preferences.collectAsStateWithLifecycle()
    val session by model.session.collectAsStateWithLifecycle()
    val transportRevision by model.transportRevision.collectAsStateWithLifecycle()
    val route by model.route.collectAsStateWithLifecycle()
    val tab by model.tab.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    // 统计页（选项卡 2、统计详情、浏览回顾、榜单）不再固定浅色，统一跟随外观设置。
    // 旧的固定浅色规则作废：用户要求夜间运行时统计页与其余页面一致。
    val dark = when (preferences.theme) {
        ThemeChoice.SYSTEM -> isSystemInDarkTheme()
        ThemeChoice.LIGHT -> false
        ThemeChoice.DARK -> true
    }
    val view = LocalView.current
    val context = LocalContext.current
    val activity = LocalActivity.current
    val imageTarget = session
    SideEffect {
        activity?.window?.let { WindowCompat.getInsetsController(it, view).apply {
            isAppearanceLightStatusBars = !dark; isAppearanceLightNavigationBars = !dark
        } }
    }
    // 只按"哪一台服务器"重建，不按传输代次（transportRevision）重建：代次变化只是 token/证书
    // 刷新，Session 本身不含凭据，取图时由 mediaClient 读当前值。否则每次重新登录都会丢掉
    // 整个已解码内存缓存，回看/再滑全部重新解码（旧版是进程级单例，没有这个代价）。
    val imageLoader = remember(session?.baseUrl, session?.instanceId) {
        ImageLoader.Builder(context).components {
            model.addThumbnailImageComponents(this)
            if (imageTarget != null) add(OkHttpNetworkFetcherFactory(
                callFactory = { model.mediaClient(imageTarget) },
                // Coil 3.1.0 默认取图器用系统联网态判定（ConnectivityCheckerApi23）。手机连着无外网
                // 的 Wi-Fi、或系统未验证网络时，即使 NAS / 隧道 / 回环仍可达也判为离线：
                // NetworkFetcher 把请求的 Cache-Control 改写成 only-if-cached，本加载器的 OkHttp
                // 客户端没有 Cache，只能合成 504，界面误报解码失败。旧版 CoilModule 用官方
                // "naive always-online" 常量 ConnectivityChecker.ONLINE 修正同一误报；真断网时请求
                // 照常发出并由 OkHttp 抛 IOException，既有错误分档不变。
                connectivityChecker = { ConnectivityChecker.ONLINE },
            ))
            if (Build.VERSION.SDK_INT >= 28) add(AnimatedImageDecoder.Factory()) else add(GifDecoder.Factory())
        }
            // 旧版 CoilModule 显式 25%（CoilModule.kt:148-152/215）。Coil 3.1.0 未配置时
            // 默认 20%（低内存机 15%）：3 列 × 三屏的 ORIGINAL 预热位图约 99×590KB≈58MB，
            // 在 256MB heap 上超过 20%（51MB）但落在 25%（64MB）内；5 列的 256×144
            // 预热约 40MB，两档都够。与旧版同值，避免三列回看时反复淘汰。
            .memoryCache {
                MemoryCache.Builder().maxSizePercent(context, MEMORY_CACHE_PERCENT).build()
            }
            .build()
    }
    DisposableEffect(imageLoader) { onDispose { imageLoader.shutdown() } }
    QimengTheme(dark) {
        CompositionLocalProvider(
            LocalMediaImages provides imageLoader,
            LocalReducedMotion provides (preferences.reducedMotion || !ValueAnimator.areAnimatorsEnabled()),
            // Content outside a Surface (page headings, plain text) must inherit the active scheme,
            // not Compose's Color.Black default, or it stays unreadable in dark mode.
            LocalContentColor provides MaterialTheme.colorScheme.onBackground,
        ) {
            if (session == null) {
                LoginScreen(model)
            } else {
                BackHandler(route != "main") { model.back() }
                // 背板采样层按材质创建一次，不随路由销毁：pushed 路由期间仍保留上一帧的录制，
                // 返回首页时底部胶囊坞的玻璃立刻有背板，不再先显示一帧平面条（原实现放在
                // route == "main" 分支里，每次 push 都销毁、返回再重录）。
                val backdrop = if (preferences.material.usesBackdrop) rememberQimengBackdropState(MaterialTheme.colorScheme.background) else null
                Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
                    Column(Modifier.fillMaxSize().then(if (backdrop != null && route == "main") Modifier.qimengBackdropSource(backdrop) else Modifier)) {
                        // 顶部安全区与路由页头都交给场景宿主：外壳不再按路由加减 padding，
                        // 常驻首页被盖住时的测量尺寸不随覆盖路由变化。
                        ResidentSceneHost(
                            activeKey = if (route == "main") "tab:$tab" else route,
                            modifier = Modifier.weight(1f),
                            chrome = { sceneKey -> SceneChrome(sceneKey, model, message) },
                        ) { sceneKey -> SceneContent(sceneKey, model, session, transportRevision, preferences) }
                    }
                    var lastTap by remember { mutableLongStateOf(0L) }
                    var lastNavigation by remember { mutableLongStateOf(0L) }
                    // 导航栏保持组合（visible=false 时不出树）：选中丸的位置记在导航栏这一层，
                    // pushed 路由返回首页时从上次位置直接出现，而不是 alpha 0 弹入。
                    LibraryNavigationBar(listOf(GlassNavItem("首页", HomeIcon, "home"), GlassNavItem("相册", AlbumIcon, "albums"),
                        GlassNavItem("数据", StatsIcon, "stats"), GlassNavItem("我的", ProfileIcon, "me")), tab, preferences.material, backdrop,
                        onSelect = { index ->
                            val now = android.os.SystemClock.elapsedRealtime()
                            when {
                                index == tab && now - lastTap <= 400L -> TabScrollController.scrollToTop(index)
                                now - lastNavigation >= 200L -> { lastNavigation = now; model.selectTab(index) }
                            }
                            lastTap = now
                        }, modifier = Modifier.align(Alignment.BottomCenter), visible = route == "main")
                }
            }
        }
    }
}

/**
 * 路由页头与 pushed 路由提示条：只渲染在活动场景内部（[ResidentSceneHost] 的 chrome 槽），
 * 不再作为常驻首页的兄弟节点占高度。判据沿用改前的外壳条件，按场景键判断：
 * Tab 场景（"tab:N"）等价于旧的 route == "main"。
 */
@Composable
private fun SceneChrome(sceneKey: String, model: LibraryViewModel, message: String?) {
    if (sceneKey.startsWith("tab:")) return
    val detailRoute = sceneKey.startsWith("asset:")
    val searchRoute = sceneKey == "search" || sceneKey.startsWith("tag:")
    val ownTopBar = sceneKey in setOf("server", "libraries", "upload", "authors", "archive-folder", "appearance", "maintenance",
        "data-manage", "thumbnail-cache", "source-rules") || sceneKey.startsWith("author:") ||
        sceneKey.startsWith("profile:") || sceneKey.startsWith("profile-archive:") || sceneKey.startsWith("asset-edit:")
    if (!detailRoute && !ownTopBar && !searchRoute && !sceneKey.startsWith("stats-detail:") &&
        sceneKey !in setOf("ai-access", "favorites", "history")) PageHeader(routeTitle(sceneKey), model::back)
    // 详情有自管的错误层，服务器页在卡片内显示提示，首页由列表内的轻提示承担：同改前。
    if (!detailRoute && sceneKey != "server" && message != null) {
        Row(Modifier.padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.weight(1f)) { ErrorNotice(message) }
            IconButton(onClick = model::clearMessage) { Icon(Icons.Outlined.Close, "关闭提示") }
        }
    }
}

/**
 * 场景内容：原 SingleSceneHost { when (route) ... } 的分支体逐条搬到此处，判据由活动
 * route 换成场景键。pushed 路由的 sceneKey 与 route 恒等；Tab 场景是 "tab:N"，必须按键
 * 里的 N 取屏，不能回落到活动 tab——常驻首页层始终以 "tab:0" 组合，若回落到 tab，切走
 * 时隐藏层会跟着渲染新 Tab。未知键维持旧 else 的默认（我的）分支。
 */
@Composable
private fun SceneContent(
    sceneKey: String,
    model: LibraryViewModel,
    session: Session?,
    transportRevision: Long,
    preferences: Preferences,
) {
    val searchRoute = sceneKey == "search" || sceneKey.startsWith("tag:")
    when {
        sceneKey.startsWith("asset:") -> DetailScreen(model, sceneKey.substringAfter(':'), session)
        sceneKey.startsWith("asset-edit:") -> {
            val id = sceneKey.substringAfter(':')
            model.targetForAssetEditor(id)?.let { target ->
                AssetEditScreen(AssetEditorTarget(target, transportRevision, id),
                    { model.closeAssetEditor(id) }, { model.closeAssetEditor(id, more = true) })
            }
        }
        sceneKey.startsWith("author:") -> AuthorCollectionScreen(model, sceneKey.substringAfter(':'))
        sceneKey.startsWith("profile:") -> AuthorProfileScreen(sceneKey.substringAfter(':'), model)
        sceneKey.startsWith("profile-archive:") -> AuthorProfileScreen(sceneKey.split(':')[1], model, archiveId = sceneKey.split(':')[2])
        sceneKey.startsWith("task:") -> TaskDetailScreen(model, sceneKey.substringAfter(':'), session)
        sceneKey.startsWith("review:") -> ReviewScreen(model, sceneKey.substringAfter(':'))
        sceneKey.startsWith("stats-detail:") -> sceneKey.split(':').let { StatsDetailScreen(model, it[1], it[2]) }
        sceneKey.startsWith("vocabulary:") -> VocabularyScreen(sceneKey.substringAfter(':'))
        sceneKey == "source-rules" -> session?.let { ExtractionVocabularyScreen(it, transportRevision, model::back) }
        sceneKey.startsWith("collection:") -> MediaBrowser(model, sceneKey, collectionId = sceneKey.substringAfter(':'))
        sceneKey.startsWith("ranking:") -> ActivityRankingScreen(model, sceneKey.split(':')[1], sceneKey.split(':')[2])
        searchRoute -> SearchScreen(model, initialTag = sceneKey.substringAfter(':', "").takeIf(String::isNotEmpty))
        sceneKey == "favorites" -> MediaBrowser(model, "favorites", favorite = true, albumTitle = "收藏")
        sceneKey == "authors" -> AuthorDirectoryScreen(model)
        sceneKey == "history" -> MediaBrowser(model, "history", source = "history", albumTitle = "浏览历史")
        sceneKey == "upload" -> UploadScreen(model)
        sceneKey == "archive-folder" -> ArchiveSettingsScreen(model::back)
        sceneKey == "maintenance" -> MaintenanceScreen(model, model::back, { model.navigate("upload") }, { model.navigate("archive-folder") }, { DeviceDeliveryScreen() })
        sceneKey == "reports" -> TasksScreen(model)
        sceneKey == "appearance" -> AppearanceScreen(preferences.theme, preferences.material,
            { model.savePreferences(preferences.copy(theme = it)) }, { model.savePreferences(preferences.copy(material = it)) }, model::back)
        sceneKey == "server" -> ServerSettingsScreen(model)
        sceneKey == "libraries" -> LibraryManagementScreen(model::back)
        sceneKey == "data-manage" -> DataManageScreen(model)
        sceneKey == "thumbnail-cache" -> ThumbnailCacheScreen(model::back)
        sceneKey == "vocabulary-manage" -> VocabularyManageScreen(model)
        sceneKey == "trash" -> TrashScreen(model)
        sceneKey == "recommendation" -> RecommendationSettings(model)
        sceneKey == "ai-access" -> AIAccessScreen(model)
        else -> when (sceneKey) {
            "tab:0" -> HomeScreen(model)
            "tab:1" -> AlbumsScreen(model)
            "tab:2" -> StatisticsScreen(model)
            else -> ProfileScreen(model)
        }
    }
}

@Composable
fun PageHeader(title: String, onBack: (() -> Unit)? = null, backEnabled: Boolean = true, actions: @Composable RowScope.() -> Unit = {}) {
    Row(Modifier.fillMaxWidth().heightIn(min = 60.dp).padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
        if (onBack != null) IconButton(onClick = onBack, enabled = backEnabled) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, "返回") }
        Text(title, Modifier.weight(1f).padding(horizontal = 8.dp), style = MaterialTheme.typography.titleLarge)
        actions()
    }
}

private fun routeTitle(route: String) = when {
    route.startsWith("asset:") -> "作品详情"
    route.startsWith("author:") -> "作者档案"
    route.startsWith("profile:") -> "作者资料"
    route.startsWith("profile-archive:") -> "保留的历史档案"
    route.startsWith("task:") -> "任务报告"
    route.startsWith("review:") -> "浏览回顾"
    route.startsWith("vocabulary:") -> vocabularyTitle(route.substringAfter(':'))
    route.startsWith("collection:") -> "同组作品"
    route.startsWith("ranking:") -> "浏览统计详情"
    else -> mapOf("search" to "搜索", "favorites" to "收藏", "authors" to "作者", "history" to "浏览历史", "upload" to "上传",
        "reports" to "任务中心", "appearance" to "外观", "server" to "服务器", "libraries" to "库管理", "data-manage" to "数据管理", "vocabulary-manage" to "词表维护", "trash" to "回收站", "recommendation" to "推荐偏好")[route] ?: "绮梦影库"
}
