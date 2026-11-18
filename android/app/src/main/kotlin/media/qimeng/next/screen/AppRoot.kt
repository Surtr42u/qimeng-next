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
import coil3.gif.AnimatedImageDecoder
import coil3.gif.GifDecoder
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.ThemeChoice
import media.qimeng.next.ui.*
import media.qimeng.next.ui.icon.HomeIcon
import media.qimeng.next.ui.icon.AlbumIcon
import media.qimeng.next.ui.icon.StatsIcon
import media.qimeng.next.ui.icon.ProfileIcon

val LocalMediaImages = staticCompositionLocalOf<ImageLoader> { error("ImageLoader missing") }

@Composable
fun AppRoot(model: LibraryViewModel) {
    val preferences by model.preferences.collectAsStateWithLifecycle()
    val session by model.session.collectAsStateWithLifecycle()
    val transportRevision by model.transportRevision.collectAsStateWithLifecycle()
    val route by model.route.collectAsStateWithLifecycle()
    val tab by model.tab.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    val queueError by model.queueError.collectAsStateWithLifecycle()
    val dark = when (preferences.theme) { ThemeChoice.SYSTEM -> isSystemInDarkTheme(); ThemeChoice.LIGHT -> false; ThemeChoice.DARK -> true }
    val view = LocalView.current
    val context = LocalContext.current
    val activity = LocalActivity.current
    val imageTarget = session
    SideEffect {
        activity?.window?.let { WindowCompat.getInsetsController(it, view).apply {
            isAppearanceLightStatusBars = !dark; isAppearanceLightNavigationBars = !dark
        } }
    }
    val imageLoader = remember(session, transportRevision) {
        ImageLoader.Builder(context).components {
            if (imageTarget != null) add(OkHttpNetworkFetcherFactory(callFactory = { model.mediaClient(imageTarget) }))
            if (Build.VERSION.SDK_INT >= 28) add(AnimatedImageDecoder.Factory()) else add(GifDecoder.Factory())
        }.build()
    }
    DisposableEffect(imageLoader) { onDispose { imageLoader.shutdown() } }
    QimengTheme(dark) {
        CompositionLocalProvider(LocalMediaImages provides imageLoader, LocalReducedMotion provides (preferences.reducedMotion || !ValueAnimator.areAnimatorsEnabled())) {
            if (session == null) {
                LoginScreen(model)
            } else {
                BackHandler(route != "main") { model.back() }
                val backdrop = rememberGlassBackdrop()
                val detailRoute = route.startsWith("asset:")
                Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background).then(if (detailRoute || route in setOf("server", "libraries", "upload")) Modifier else Modifier.statusBarsPadding())) {
                    Column(Modifier.fillMaxSize().captureGlassBackdrop(backdrop)) {
                        if (!detailRoute && !route.startsWith("stats-detail:") && route !in setOf("main", "ai-access", "search", "server", "favorites", "libraries", "upload", "history")) PageHeader(routeTitle(route), model::back)
                        if (!detailRoute && route != "server" && message != null) {
                            Row(Modifier.padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                                Box(Modifier.weight(1f)) { ErrorNotice(message!!) }
                                IconButton(onClick = model::clearMessage) { Icon(Icons.Outlined.Close, "关闭提示") }
                            }
                        }
                        if (!detailRoute && queueError != null && route !in setOf("upload", "server")) Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                            Text(queueError!!, Modifier.weight(1f), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                            TextButton(onClick = { model.navigate("upload") }) { Text("查看传输") }
                        }
                        SingleSceneHost(if (route == "main") "tab:$tab" else route, preferences.reducedMotion, Modifier.weight(1f)) {
                            when {
                                route.startsWith("asset:") -> DetailScreen(model, route.substringAfter(':'))
                                route.startsWith("author:") -> AuthorDetailScreen(model, route.substringAfter(':'))
                                route.startsWith("profile:") -> AuthorProfileScreen(route.substringAfter(':'), model)
                                route.startsWith("profile-archive:") -> AuthorProfileScreen(route.split(':')[1], model, archiveId = route.split(':')[2])
                                route.startsWith("task:") -> TaskDetailScreen(model, route.substringAfter(':'))
                                route.startsWith("review:") -> ReviewScreen(model, route.substringAfter(':'))
                                route.startsWith("stats-detail:") -> route.split(':').let { StatsDetailScreen(model, it[1], it[2]) }
                                route.startsWith("vocabulary:") -> VocabularyScreen(route.substringAfter(':'))
                                route.startsWith("collection:") -> MediaBrowser(model, route, collectionId = route.substringAfter(':'))
                                route.startsWith("tag:") -> MediaBrowser(model, route, initialTag = route.substringAfter(':'))
                                route.startsWith("ranking:") -> ActivityRankingScreen(model, route.split(':')[1], route.split(':')[2])
                                route == "search" -> SearchScreen(model)
                                route == "favorites" -> MediaBrowser(model, "favorites", favorite = true, albumTitle = "收藏")
                                route == "authors" -> AuthorsScreen(model)
                                route == "history" -> MediaBrowser(model, "history", source = "history", albumTitle = "浏览历史")
                                route == "upload" -> UploadScreen(model)
                                route == "reports" -> TasksScreen(model)
                                route == "appearance" -> AppearanceScreen(model)
                                route == "server" -> ServerSettingsScreen(model)
                                route == "libraries" -> LibraryManagementScreen(model::back)
                                route == "data-manage" -> DataManageScreen(model)
                                route == "vocabulary-manage" -> VocabularyManageScreen(model)
                                route == "trash" -> TrashScreen(model)
                                route == "recommendation" -> RecommendationSettings(model)
                                route == "ai-access" -> AIAccessScreen(model)
                                else -> when (tab) {
                                    0 -> HomeScreen(model)
                                    1 -> AlbumsScreen(model)
                                    2 -> StatisticsScreen(model)
                                    else -> ProfileScreen(model)
                                }
                            }
                        }
                    }
                    if (route == "main") LiquidNavigation(
                        items = listOf(NavigationItem("home", "首页", HomeIcon), NavigationItem("albums", "相册", AlbumIcon),
                            NavigationItem("stats", "数据", StatsIcon), NavigationItem("me", "我的", ProfileIcon)),
                        selected = tab, backdrop = backdrop, reducedMotion = preferences.reducedMotion, glass = preferences.glass,
                        onSelect = model::selectTab,
                        modifier = Modifier.align(Alignment.BottomCenter).navigationBarsPadding()
                            .padding(horizontal = NavigationDimensions.horizontalMargin).padding(bottom = NavigationDimensions.bottomMargin).fillMaxWidth())
                }
            }
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
    route.startsWith("tag:") -> "标签：${route.substringAfter(':')}"
    route.startsWith("ranking:") -> "浏览统计详情"
    else -> mapOf("search" to "搜索", "favorites" to "收藏", "authors" to "作者", "history" to "浏览历史", "upload" to "上传",
        "reports" to "任务中心", "appearance" to "外观", "server" to "服务器", "libraries" to "库管理", "data-manage" to "数据管理", "vocabulary-manage" to "词表维护", "trash" to "回收站", "recommendation" to "推荐偏好")[route] ?: "绮梦影库"
}
