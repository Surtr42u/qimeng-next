package media.qimeng.next.screen

import android.app.Activity
import android.animation.ValueAnimator
import android.os.Build
import androidx.activity.compose.BackHandler
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

val LocalMediaImages = staticCompositionLocalOf<ImageLoader> { error("ImageLoader missing") }

@Composable
fun AppRoot(model: LibraryViewModel) {
    val preferences by model.preferences.collectAsStateWithLifecycle()
    val session by model.session.collectAsStateWithLifecycle()
    val route by model.route.collectAsStateWithLifecycle()
    val tab by model.tab.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    val queueError by model.queueError.collectAsStateWithLifecycle()
    val dark = when (preferences.theme) { ThemeChoice.SYSTEM -> isSystemInDarkTheme(); ThemeChoice.LIGHT -> false; ThemeChoice.DARK -> true }
    val view = LocalView.current
    val context = LocalContext.current
    val imageTarget = session
    SideEffect {
        (context as? Activity)?.window?.let { WindowCompat.getInsetsController(it, view).isAppearanceLightStatusBars = !dark }
    }
    val imageLoader = remember(session) {
        ImageLoader.Builder(context).components {
            if (imageTarget != null) add(OkHttpNetworkFetcherFactory(callFactory = { model.mediaClient(imageTarget) }))
            if (Build.VERSION.SDK_INT >= 28) add(AnimatedImageDecoder.Factory()) else add(GifDecoder.Factory())
        }.build()
    }
    DisposableEffect(imageLoader) { onDispose { imageLoader.shutdown() } }
    QimengTheme(dark) {
        CompositionLocalProvider(LocalMediaImages provides imageLoader) {
            if (session == null) {
                LoginScreen(model)
            } else {
                BackHandler(route != "main") { model.back() }
                val backdrop = rememberGlassBackdrop()
                Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background).statusBarsPadding()) {
                    Column(Modifier.fillMaxSize().captureGlassBackdrop(backdrop)) {
                        if (route != "main") PageHeader(routeTitle(route), model::back)
                        if (message != null) {
                            Row(Modifier.padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                                Box(Modifier.weight(1f)) { ErrorNotice(message!!) }
                                IconButton(onClick = model::clearMessage) { Icon(Icons.Outlined.Close, "关闭提示") }
                            }
                        }
                        if (queueError != null && route != "upload") Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                            Text(queueError!!, Modifier.weight(1f), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                            TextButton(onClick = { model.navigate("upload") }) { Text("查看传输") }
                        }
                        Box(Modifier.fillMaxWidth().height(4.dp)) { if (busy) LinearProgressIndicator(Modifier.fillMaxWidth()) }
                        SingleSceneHost(if (route == "main") "tab:$tab" else route, preferences.reducedMotion, Modifier.weight(1f)) {
                            when {
                                route.startsWith("asset:") -> DetailScreen(model, route.substringAfter(':'))
                                route.startsWith("author:") -> AuthorDetailScreen(model, route.substringAfter(':'))
                                route.startsWith("task:") -> TaskDetailScreen(model, route.substringAfter(':'))
                                route.startsWith("review:") -> ReviewScreen(model, route.substringAfter(':'))
                                route.startsWith("vocabulary:") -> VocabularyScreen(route.substringAfter(':'))
                                route.startsWith("collection:") -> MediaBrowser(model, route, collectionId = route.substringAfter(':'))
                                route.startsWith("tag:") -> MediaBrowser(model, route, initialTag = route.substringAfter(':'))
                                route.startsWith("ranking:") -> ActivityRankingScreen(model, route.split(':')[1], route.split(':')[2])
                                route == "search" -> MediaBrowser(model, "search", search = true)
                                route == "favorites" -> MediaBrowser(model, "favorites", favorite = true)
                                route == "authors" -> AuthorsScreen(model)
                                route == "history" -> HistoryScreen(model)
                                route == "upload" -> UploadScreen(model)
                                route == "reports" -> TasksScreen(model)
                                route == "appearance" -> AppearanceScreen(model)
                                route == "server" -> ServerScreen(model)
                                route == "trash" -> TrashScreen(model)
                                route == "recommendation" -> RecommendationSettings(model)
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
                        items = listOf(NavigationItem("home", "首页", Icons.Outlined.Home), NavigationItem("albums", "相册", Icons.Outlined.PhotoLibrary),
                            NavigationItem("stats", "数据", Icons.Outlined.BarChart), NavigationItem("me", "我的", Icons.Outlined.Person)),
                        selected = tab, backdrop = backdrop, reducedMotion = preferences.reducedMotion, glass = preferences.glass,
                        onSelect = model::selectTab,
                        modifier = Modifier.align(Alignment.BottomCenter).navigationBarsPadding().padding(horizontal = 24.dp, vertical = 12.dp).widthIn(max = 480.dp).fillMaxWidth())
                }
            }
        }
    }
}

@Composable
private fun LoginScreen(model: LibraryViewModel) {
    var address by rememberSaveable { mutableStateOf("") }
    // Passwords never enter saved instance state.
    var token by remember { mutableStateOf("") }
    val busy by model.busy.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    val settingsError by model.settingsError.collectAsStateWithLifecycle()
    Column(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background).safeDrawingPadding().imePadding()
        .verticalScroll(rememberScrollState()).padding(24.dp), verticalArrangement = Arrangement.spacedBy(20.dp)) {
        Spacer(Modifier.height(48.dp))
        Text("绮梦影库", style = MaterialTheme.typography.displaySmall)
        Text("连接 NAS 或电脑，浏览你的同一座影库。", color = MaterialTheme.colorScheme.onSurfaceVariant)
        ContentCard {
            Text("服务器", style = MaterialTheme.typography.titleLarge)
            OutlinedTextField(address, { address = it }, label = { Text("HTTPS 服务器地址") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            OutlinedTextField(token, { token = it }, label = { Text("登录令牌") }, visualTransformation = PasswordVisualTransformation(), singleLine = true, modifier = Modifier.fillMaxWidth())
            Button(onClick = { model.login(address, token) }, enabled = !busy && address.isNotBlank() && token.isNotBlank(), modifier = Modifier.fillMaxWidth()) {
                if (busy) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) else Text("连接影库")
            }
            Text("地址与登录由你指定。通过本机隧道连接时，可填写 localhost 地址。", style = MaterialTheme.typography.bodySmall)
        }
        (message ?: settingsError)?.let { ErrorNotice(it) }
    }
}

@Composable
fun PageHeader(title: String, onBack: (() -> Unit)? = null, actions: @Composable RowScope.() -> Unit = {}) {
    Row(Modifier.fillMaxWidth().heightIn(min = 60.dp).padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
        if (onBack != null) IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, "返回") }
        Text(title, Modifier.weight(1f).padding(horizontal = 8.dp), style = MaterialTheme.typography.titleLarge)
        actions()
    }
}

private fun routeTitle(route: String) = when {
    route.startsWith("asset:") -> "作品详情"
    route.startsWith("author:") -> "作者档案"
    route.startsWith("task:") -> "任务报告"
    route.startsWith("review:") -> "浏览回顾"
    route.startsWith("vocabulary:") -> vocabularyTitle(route.substringAfter(':'))
    route.startsWith("collection:") -> "同组作品"
    route.startsWith("tag:") -> "标签：${route.substringAfter(':')}"
    route.startsWith("ranking:") -> "浏览统计详情"
    else -> mapOf("search" to "搜索", "favorites" to "收藏", "authors" to "作者", "history" to "浏览历史", "upload" to "上传",
        "reports" to "任务中心", "appearance" to "外观", "server" to "服务器", "trash" to "回收站", "recommendation" to "推荐偏好")[route] ?: "绮梦影库"
}
