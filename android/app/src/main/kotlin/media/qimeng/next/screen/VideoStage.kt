package media.qimeng.next.screen

import android.content.pm.ActivityInfo
import android.os.SystemClock
import androidx.activity.compose.BackHandler
import androidx.activity.compose.LocalActivity
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.BookmarkAdd
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.painter.ColorPainter
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.util.VelocityTracker
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.datasource.HttpDataSource
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import coil3.compose.AsyncImage
import coil3.request.ImageRequest
import kotlinx.coroutines.delay
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.PlaybackHistory
import media.qimeng.next.model.Asset
import media.qimeng.next.screen.video.BiliPlayerView
import media.qimeng.next.ui.icon.PlayIcon
import kotlin.math.abs

@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VideoStage(
    asset: Asset,
    model: LibraryViewModel,
    modifier: Modifier = Modifier,
    onPlayerActiveChanged: (Boolean) -> Unit = {},
    onPlaybackChromeChanged: (Boolean) -> Unit = {},
) {
    // Each surface, saved position, and event listener belongs to one source version on one server.
    key(asset.target.baseUrl, asset.target.instanceId, asset.id, asset.version) {
        VersionedVideoStage(asset, model, modifier, onPlayerActiveChanged, onPlaybackChromeChanged)
    }
}

@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun VersionedVideoStage(
    asset: Asset,
    model: LibraryViewModel,
    modifier: Modifier,
    onPlayerActiveChanged: (Boolean) -> Unit,
    onPlaybackChromeChanged: (Boolean) -> Unit,
) {
    val context = LocalContext.current
    val activity = LocalActivity.current
    val owner = LocalLifecycleOwner.current
    val viewingSession = remember { "${model.viewSessionId()}:${asset.version}" }
    var activated by remember { mutableStateOf(false) }
    var timeline by remember { mutableStateOf(false) }
    var controlsVisible by remember { mutableStateOf(true) }
    var fullscreen by remember { mutableStateOf(false) }
    var enteredFullscreen by remember { mutableStateOf(false) }
    var lastFullscreenAt by remember { mutableLongStateOf(-FULLSCREEN_DEBOUNCE_MS) }
    var savedPosition by rememberSaveable {
        mutableLongStateOf(if (asset.positionVersion == asset.version) asset.positionMs else 0)
    }
    var error by remember { mutableStateOf<String?>(null) }
    var staleContent by remember { mutableStateOf(false) }
    var pendingPlay by remember { mutableStateOf(false) }
    var surfaceReady by remember { mutableStateOf(false) }
    var playerView by remember { mutableStateOf<BiliPlayerView?>(null) }
    val player = remember {
        val source = OkHttpDataSource.Factory(model.mediaClient(asset.target))
        ExoPlayer.Builder(context).setMediaSourceFactory(DefaultMediaSourceFactory(source)).build().apply {
            volume = 0f
        }
    }
    val history = remember(player) {
        PlaybackHistory(
            player,
            { model.recordPlay(asset, viewingSession) },
            { position -> model.recordProgress(asset, position, viewingSession) },
        )
    }
    val duration = player.duration.takeIf { it > 0L } ?: asset.durationMs
    val watched = duration != null && duration > 0L && savedPosition >= duration
    val canFullscreen = activity != null && asset.width?.let { width ->
        asset.height?.let { height -> height > 0 && width > height }
    } == true
    val latestCanFullscreen by rememberUpdatedState(canFullscreen)

    fun setFullscreen(enter: Boolean) {
        if (enter) {
            val now = SystemClock.elapsedRealtime()
            if (fullscreen || !latestCanFullscreen || now - lastFullscreenAt < FULLSCREEN_DEBOUNCE_MS) return
            lastFullscreenAt = now
            enteredFullscreen = true
        }
        fullscreen = enter
    }

    fun checkpoint() {
        if (player.currentMediaItem != null) savedPosition = player.currentPosition
        history.checkpoint()
    }

    DisposableEffect(activity) {
        val original = activity?.requestedOrientation
        onDispose {
            if (activity != null && original != null && enteredFullscreen && activity.requestedOrientation != original) {
                activity.requestedOrientation = original
            }
        }
    }
    LaunchedEffect(activity, enteredFullscreen, fullscreen) {
        if (activity != null && enteredFullscreen) {
            val orientation = if (fullscreen) ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE else ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
            if (activity.requestedOrientation != orientation) activity.requestedOrientation = orientation
        }
    }

    BackHandler(activated && !timeline) {
        if (fullscreen) {
            setFullscreen(false)
        } else {
            pendingPlay = false
            player.pause()
            checkpoint()
            activated = false
        }
    }
    DisposableEffect(player, owner) {
        var resumePlayback = false
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_PAUSE -> {
                    resumePlayback = player.isPlaying
                    pendingPlay = false
                    player.pause()
                    checkpoint()
                }
                Lifecycle.Event.ON_RESUME -> {
                    pendingPlay = resumePlayback && activated
                    resumePlayback = false
                }
                else -> Unit
            }
        }
        val listener = object : Player.Listener {
            override fun onSurfaceSizeChanged(width: Int, height: Int) {
                surfaceReady = width > 0 && height > 0
            }

            override fun onPlayerError(failure: PlaybackException) {
                checkpoint()
                staleContent = generateSequence<Throwable>(failure) { it.cause }
                    .filterIsInstance<HttpDataSource.InvalidResponseCodeException>()
                    .any { it.responseCode == 409 }
                error = if (staleContent) "媒体内容已更新，点按重新读取" else "播放失败，点按重试"
                pendingPlay = false
                timeline = false
                fullscreen = false
                activated = false
            }
        }
        owner.lifecycle.addObserver(observer)
        player.addListener(listener)
        player.addListener(history)
        onDispose {
            checkpoint()
            owner.lifecycle.removeObserver(observer)
            player.removeListener(listener)
            player.removeListener(history)
            playerView?.detachPlayer()
            player.release()
        }
    }

    LaunchedEffect(activated, surfaceReady, pendingPlay) {
        // Hold off preparation until Media3 has an attached output with real dimensions.
        if (activated && pendingPlay && surfaceReady && player.surfaceSize.width > 0 &&
            player.surfaceSize.height > 0 && owner.lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)
        ) {
            when {
                player.playbackState == Player.STATE_IDLE -> {
                    val start = if (watched) 0L else savedPosition
                    player.setMediaItem(MediaItem.fromUri(asset.contentUrl), start)
                    player.prepare()
                }
                player.playbackState == Player.STATE_ENDED -> player.seekTo(0L)
            }
            player.play()
            pendingPlay = false
        }
    }
    LaunchedEffect(activated) {
        if (!activated) return@LaunchedEffect
        while (true) {
            delay(10_000)
            if (player.isPlaying) checkpoint()
        }
    }
    SideEffect {
        onPlayerActiveChanged(activated)
        onPlaybackChromeChanged(!activated || controlsVisible)
    }

    Box(modifier) {
        if (!activated) {
            VideoPoster(asset, watched, error, model::adjacentAsset) {
                error = null
                if (staleContent) {
                    model.refreshAsset(asset.id)
                } else {
                    controlsVisible = true
                    pendingPlay = true
                    activated = true
                }
            }
        } else {
            AndroidView(
                modifier = Modifier.fillMaxSize(),
                factory = { ctx ->
                    BiliPlayerView(ctx).apply {
                        onFullscreen = ::setFullscreen
                        onControllerVisibilityChanged = { controlsVisible = it }
                        setPlayer(player)
                    }.also { playerView = it }
                },
                update = { it.setFullscreen(fullscreen, canFullscreen) },
                onRelease = {
                    it.detachPlayer()
                    if (playerView === it) playerView = null
                },
            )
            if (controlsVisible && !fullscreen) {
                IconButton(
                    onClick = { timeline = true; player.pause() },
                    modifier = Modifier.align(Alignment.TopStart).safeDrawingPadding().padding(8.dp),
                ) {
                    Icon(Icons.Outlined.BookmarkAdd, "时间轴标记", tint = Color.White)
                }
            }
        }
    }
    if (timeline) ModalBottomSheet(onDismissRequest = { timeline = false }) { TimelineControls(asset, player) }
}

// Poster and swipe thresholds from the installed app's feature/detail/VideoStage.kt.
@Composable
private fun VideoPoster(
    asset: Asset,
    watched: Boolean,
    error: String?,
    onNavigate: (Int) -> Unit,
    onPlay: () -> Unit,
) {
    val context = LocalContext.current
    val swipeDistance = with(LocalDensity.current) { 60.dp.toPx() }
    val latestOnNavigate by rememberUpdatedState(onNavigate)
    Box(
        Modifier.fillMaxSize()
            .pointerInput(swipeDistance) {
                var distance = 0f
                val tracker = VelocityTracker()
                detectHorizontalDragGestures(
                    onDragStart = {
                        distance = 0f
                        tracker.resetTracking()
                    },
                    onHorizontalDrag = { change, delta ->
                        tracker.addPosition(change.uptimeMillis, change.position)
                        distance += delta
                        change.consume()
                    },
                    onDragEnd = {
                        if (abs(distance) > swipeDistance || abs(tracker.calculateVelocity().x) > 800f) {
                            latestOnNavigate(if (distance < 0f) 1 else -1)
                        }
                    },
                )
            }
            .clickable(onClick = onPlay)
            .semantics { contentDescription = "视频播放区域" },
    ) {
        AsyncImage(
            model = remember(asset.thumbnailUrl, asset.target, asset.id, asset.version) {
                val imageKey = "${asset.target.baseUrl}:${asset.target.instanceId}:${asset.id}:${asset.version}:thumbnail"
                ImageRequest.Builder(context).data(asset.thumbnailUrl)
                    .memoryCacheKey(imageKey).diskCacheKey(imageKey).build()
            },
            imageLoader = LocalMediaImages.current,
            contentDescription = null,
            contentScale = ContentScale.Fit,
            placeholder = ColorPainter(MaterialTheme.colorScheme.secondaryContainer),
            modifier = Modifier.fillMaxSize(),
        )
        Surface(
            shape = CircleShape,
            color = Color.Black.copy(alpha = 0.6f),
            modifier = Modifier.align(Alignment.Center).size(64.dp),
        ) {
            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                Icon(PlayIcon, "播放视频", tint = Color.White)
            }
        }
        if (watched || error != null) {
            Surface(
                shape = RoundedCornerShape(4.dp),
                color = Color.Black.copy(alpha = 0.6f),
                modifier = Modifier.align(if (error != null) Alignment.TopCenter else Alignment.TopStart)
                    .safeDrawingPadding().padding(12.dp),
            ) {
                Text(error ?: "已看完", Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
                    color = Color.White, style = MaterialTheme.typography.labelSmall)
            }
        }
    }
}

private const val FULLSCREEN_DEBOUNCE_MS = 800L
