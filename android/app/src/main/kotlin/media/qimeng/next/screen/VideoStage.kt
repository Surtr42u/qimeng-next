package media.qimeng.next.screen

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.media.AudioManager
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Fullscreen
import androidx.compose.material.icons.outlined.FullscreenExit
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.compose.ui.window.DialogWindowProvider
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.datasource.HttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.ui.PlayerView
import kotlinx.coroutines.delay
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Asset

/** The sole platform-view bridge uses the official Media3 player; gesture orchestration is new Compose code. */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
@Composable
fun VideoStage(asset: Asset, model: LibraryViewModel) {
    val context = LocalContext.current
    val owner = LocalLifecycleOwner.current
    val viewingSession = remember(asset.id) { model.viewSessionId() }
    var fullscreen by rememberSaveable(asset.id) { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var staleContent by remember { mutableStateOf(false) }
    val player = remember(asset.id, asset.version) {
        ExoPlayer.Builder(context).setMediaSourceFactory(DefaultMediaSourceFactory(OkHttpDataSource.Factory(model.mediaClient())))
            .build().apply {
                setMediaItem(MediaItem.fromUri(asset.contentUrl))
                if (asset.positionVersion == asset.version) seekTo(asset.positionMs)
                prepare()
            }
    }
    val currentAsset by rememberUpdatedState(asset)
    DisposableEffect(player, owner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_PAUSE) {
                player.pause()
                model.recordProgress(currentAsset, player.currentPosition, viewingSession)
            }
        }
        val listener = object : Player.Listener {
            override fun onPlayerError(failure: PlaybackException) {
                staleContent = generateSequence<Throwable>(failure) { it.cause }.filterIsInstance<HttpDataSource.InvalidResponseCodeException>().any { it.responseCode == 409 }
                error = if (staleContent) "媒体内容已更新，请重新读取" else "视频无法播放，请检查网络或媒体格式"
            }
            override fun onIsPlayingChanged(isPlaying: Boolean) { if (isPlaying) model.recordPlay(currentAsset, viewingSession) }
        }
        owner.lifecycle.addObserver(observer); player.addListener(listener)
        onDispose {
            model.recordProgress(currentAsset, player.currentPosition, viewingSession)
            owner.lifecycle.removeObserver(observer); player.removeListener(listener); player.release()
        }
    }
    LaunchedEffect(player) {
        while (true) {
            delay(10_000)
            if (player.isPlaying) model.recordProgress(currentAsset, player.currentPosition, viewingSession)
        }
    }
    if (fullscreen) {
        Dialog(onDismissRequest = { fullscreen = false }, properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
            BackHandler { fullscreen = false }
            PlayerSurface(player, true, error, { error = null; if (staleContent) model.openAsset(asset.id) else player.prepare() }) { fullscreen = false }
        }
        Box(Modifier.fillMaxWidth().aspectRatio(16f / 9f).background(Color.Black))
    } else PlayerSurface(player, false, error, { error = null; if (staleContent) model.openAsset(asset.id) else player.prepare() }) { fullscreen = true }
    TimelineControls(asset, player)
}

@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
@Composable
private fun PlayerSurface(player: ExoPlayer, fullscreen: Boolean, error: String?, onRetry: () -> Unit, toggleFullscreen: () -> Unit) {
    val context = LocalContext.current
    var feedback by remember { mutableStateOf<String?>(null) }
    var playing by remember(player) { mutableStateOf(player.isPlaying) }
    var showControls by remember { mutableStateOf(true) }
    DisposableEffect(player) {
        val listener = object : Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) { playing = isPlaying; if (!isPlaying) showControls = true }
        }
        player.addListener(listener)
        onDispose { player.removeListener(listener) }
    }
    val audio = remember { context.getSystemService(Context.AUDIO_SERVICE) as AudioManager }
    val view = LocalView.current
    val window = (view.parent as? DialogWindowProvider)?.window ?: context.activity()?.window
    DisposableEffect(window) {
        val original = window?.attributes?.screenBrightness
        onDispose { if (window != null && original != null) window.attributes = window.attributes.apply { screenBrightness = original } }
    }
    Box((if (fullscreen) Modifier.fillMaxSize() else Modifier.fillMaxWidth().aspectRatio(16f / 9f)).background(Color.Black)
        .semantics { contentDescription = "视频播放区域" }) {
        AndroidView(factory = { ctx -> PlayerView(ctx).apply {
            this.player = player; useController = true; controllerAutoShow = true
            setShowBuffering(PlayerView.SHOW_BUFFERING_WHEN_PLAYING)
            setControllerHideOnTouch(false)
        } }, update = {
            it.player = player; it.controllerShowTimeoutMs = if (playing) 3000 else 0
            if (showControls || !playing) it.showController() else it.hideController()
        },
            onRelease = { it.player = null }, modifier = Modifier.fillMaxSize())
        // Keep the bottom native seek/control strip available; the stage owns playback gestures.
        Box(Modifier.fillMaxWidth().fillMaxHeight(0.68f)
            .pointerInput(player) { detectTapGestures(onTap = { showControls = !showControls }, onDoubleTap = { if (player.isPlaying) player.pause() else player.play() }) }
            .pointerInput(player, fullscreen) {
                var startX = 0f; var dx = 0f; var dy = 0f; var position = 0L; var brightness = 0.5f; var volume = 0
                var direction = 0
                detectDragGestures(onDragStart = {
                    startX = it.x; dx = 0f; dy = 0f; direction = 0; position = player.currentPosition
                    brightness = window?.attributes?.screenBrightness?.takeIf { it >= 0 } ?: 0.5f
                    volume = audio.getStreamVolume(AudioManager.STREAM_MUSIC)
                }, onDragEnd = {
                    if (direction == 1) player.seekTo((position + (dx / size.width * 120_000).toLong()).coerceIn(0, player.duration.coerceAtLeast(0)))
                    feedback = null
                }, onDragCancel = { feedback = null }) { change, delta ->
                    change.consume(); dx += delta.x; dy += delta.y
                    if (direction == 0) direction = if (kotlin.math.abs(dx) > kotlin.math.abs(dy)) 1 else 2
                    if (direction == 1) feedback = "快进 / 后退 ${((dx / size.width * 120_000) / 1000).toInt()} 秒"
                    else if (startX < size.width / 2) {
                        val value = (brightness - dy / size.height).coerceIn(0.02f, 1f)
                        if (window != null) window.attributes = window.attributes.apply { screenBrightness = value }
                        feedback = "亮度 ${(value * 100).toInt()}%"
                    } else {
                        val maximum = audio.getStreamMaxVolume(AudioManager.STREAM_MUSIC)
                        val value = (volume - dy / size.height * maximum).toInt().coerceIn(0, maximum)
                        audio.setStreamVolume(AudioManager.STREAM_MUSIC, value, 0)
                        feedback = "音量 $value / $maximum"
                    }
                }
            })
        IconButton(onClick = toggleFullscreen, modifier = Modifier.align(Alignment.TopEnd).safeDrawingPadding().padding(8.dp)) {
            Icon(if (fullscreen) Icons.Outlined.FullscreenExit else Icons.Outlined.Fullscreen, if (fullscreen) "退出全屏" else "进入全屏", tint = Color.White)
        }
        feedback?.let { Text(it, Modifier.align(Alignment.Center).background(Color.Black.copy(alpha = 0.65f)).padding(12.dp), color = Color.White) }
        error?.let { TextButton(onClick = onRetry, modifier = Modifier.align(Alignment.Center)) { Text("$it · 重试", color = Color.White) } }
    }
}

private fun Context.activity(): Activity? {
    var context: Context = this
    while (context is ContextWrapper) {
        if (context is Activity) return context
        context = context.baseContext
    }
    return context as? Activity
}
