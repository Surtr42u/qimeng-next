package media.qimeng.next.test

import android.app.Activity
import android.content.pm.ActivityInfo
import android.content.res.Configuration
import android.graphics.Bitmap
import android.graphics.Rect
import android.net.Uri
import android.os.SystemClock
import android.view.MotionEvent
import android.view.SurfaceView
import android.view.View
import android.widget.TextView
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.AndroidComposeTestRule
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModelProvider
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import androidx.test.ext.junit.rules.ActivityScenarioRule
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.MainActivity
import media.qimeng.next.R
import media.qimeng.next.data.DataGraph
import media.qimeng.next.model.Asset
import media.qimeng.next.model.TransferState
import media.qimeng.next.model.UploadMode
import media.qimeng.next.testsupport.captureCloudScreenshot
import org.junit.Assert.*

internal typealias VideoTestRule = AndroidComposeTestRule<ActivityScenarioRule<MainActivity>, MainActivity>

@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
internal class VideoParityRoundTrip(private val compose: VideoTestRule, private val graph: DataGraph) {
    fun run() {
        val (landscape, portrait, authorId) = seed()
        compose.onNodeWithTag("navigation:home").performTouchInput { click(center) }
        openFromGrid("home:0", landscape)
        captureFrame("installed-video-home-poster.png", landscape = false)
        startPlayback()
        val first = awaitPlayback { it.playing && it.position > 350L }
        assertEquals("The original player starts muted", 0f, first.volume, 0f)
        tap("开启声音")
        awaitPlayback { it.volume == 1f }
        tap("静音")
        awaitPlayback { it.volume == 0f }

        tapSurface()
        waitForControls(visible = false)
        tapSurface()
        waitForControls(visible = true)
        tapSurface(twice = true)
        val paused = awaitPlayback { !it.requestedPlay && it.position > 0L }
        awaitServer {
            val stored = graph.media.asset(landscape.id)
            stored.positionMs == paused.position && stored.playCount == 1L &&
                graph.media.history().items.any { it.assetId == landscape.id }
        }
        val eventsBefore = runBlocking { graph.media.statistics().events }
        val canvas = onActivity { first.view.screenBounds() }
        drag(canvas.left + canvas.width() * 0.15f, canvas.left + canvas.width() * 0.8f, canvas.exactCenterY()) {
            assertEquals("A drag preview must leave the paused decoder position at the original frame", paused.position, sample()?.position)
        }
        val sought = awaitPlayback { !it.requestedPlay && it.position >= paused.position + 4_000L }
        awaitServer {
            graph.media.asset(landscape.id).let { it.positionVersion == landscape.version && it.positionMs == sought.position }
        }
        assertEquals("Go should receive a single progress event for a 35-move drag",
            eventsBefore + 1L, runBlocking { graph.media.statistics().events })

        tapSurface()
        waitForControls(visible = true)
        dragSeekBar(0.85f)
        awaitPlayback { !it.requestedPlay && it.state == Player.STATE_READY && it.position in 9_500L..11_500L }
        tap("播放")
        awaitPlayback { it.state == Player.STATE_ENDED }
        assertNotNull("Finishing must show the playback controls", controlBounds("播放进度"))
        tap("播放")
        val replay = awaitPlayback { it.playing && it.position < 1_500L }

        tap("进入全屏")
        awaitPlayback {
            it.orientationRequest == ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE &&
                it.orientation == Configuration.ORIENTATION_LANDSCAPE && it.playing
        }
        waitForControls(visible = false)
        assertTrue("Fullscreen must render color on the Media3 surface", captureDecoderSurface(compose.activity, "-installed-fullscreen"))
        captureFrame("installed-video-landscape-immersive.png", landscape = true)
        tapSurface()
        waitForControls(visible = true)
        val shownAt = SystemClock.elapsedRealtime()
        val fullscreen = requireNotNull(sample())
        assertSame(first.player, fullscreen.player)
        assertSame(first.surface, fullscreen.surface)
        assertTrue("Playback time must carry forward into fullscreen", fullscreen.position >= replay.position)
        assertFullscreenInsets()
        captureFrame("installed-video-landscape-controls.png", landscape = true)
        waitForControls(visible = false)
        assertTrue("Playing controls must stay visible for roughly five seconds", SystemClock.elapsedRealtime() - shownAt >= 4_000L)

        val atExit = requireNotNull(sample())
        back()
        val returned = awaitPlayback {
            it.orientationRequest == ActivityInfo.SCREEN_ORIENTATION_PORTRAIT &&
                it.orientation == Configuration.ORIENTATION_PORTRAIT && it.position > atExit.position
        }
        assertSame(first.player, returned.player)
        assertSame(first.surface, returned.surface)
        assertTrue("The portrait surface must render after fullscreen", captureDecoderSurface(compose.activity, "-installed-returned"))
        captureFrame("installed-video-back-to-portrait.png", landscape = false)

        tap("暂停")
        dragSeekBar(0.28f)
        awaitPlayback { !it.requestedPlay && it.position in 2_600L..4_000L }
        tap("播放")
        awaitPlayback { it.playing }
        val scenario = compose.activityRule.scenario
        scenario.moveToState(Lifecycle.State.CREATED)
        val background = awaitPlayback { !it.requestedPlay }
        scenario.moveToState(Lifecycle.State.RESUMED)
        awaitPlayback { it.playing && it.position > background.position }
        tap("暂停")
        val pausedAgain = awaitPlayback { !it.requestedPlay }
        scenario.moveToState(Lifecycle.State.CREATED)
        scenario.moveToState(Lifecycle.State.RESUMED)
        compose.waitForIdle()
        assertEquals("A user-paused video must stay paused on return", pausedAgain.position, sample()?.position)
        assertFalse(requireNotNull(sample()).requestedPlay)
        back()
        awaitPoster(landscape)
        back()
        awaitServer { graph.media.asset(landscape.id).positionMs == pausedAgain.position }

        compose.onNodeWithTag("navigation:albums").performTouchInput { click(center) }
        openFromGrid("albums", landscape)
        startPlayback()
        val resumed = awaitPlayback { it.playing }
        assertNotSame("Leaving detail releases the former player", first.player, resumed.player)
        assertTrue("Albums entry must resume from Go", resumed.position in pausedAgain.position..(pausedAgain.position + 1_500L))
        back()
        awaitPoster(landscape)
        back()

        compose.onNodeWithTag("navigation:me").performTouchInput { click(center) }
        compose.onNodeWithTag("profile-settings-list").performScrollToNode(hasText("作者管理"))
        compose.onNodeWithText("作者管理").performTouchInput { click(center) }
        val author = hasText(AUTHOR_NAME) and hasClickAction()
        compose.waitUntil(20_000) { compose.onAllNodes(author).fetchSemanticsNodes().isNotEmpty() }
        compose.onNode(author).performTouchInput { click(center) }
        openFromGrid("author:$authorId", portrait)
        startPlayback()
        val vertical = awaitPlayback { it.playing && it.width == 180 && it.height == 320 }
        assertEquals(Configuration.ORIENTATION_PORTRAIT, vertical.orientation)
        val disabledFullscreen = requireNotNull(controlBounds("进入全屏", enabled = false))
        click(disabledFullscreen)
        assertEquals(vertical.orientationRequest, sample()?.orientationRequest)
        assertTrue(captureDecoderSurface(compose.activity, "-installed-vertical"))
        captureFrame("installed-video-author-vertical.png", landscape = false)
        tap("暂停")
        val stoppedVertical = awaitPlayback { !it.requestedPlay }
        holdSpeed()
        awaitPlayback { it.speed == 1f && !it.requestedPlay }
        holdSpeed("⬇ 拖动到此处锁定倍速")
        awaitPlayback { it.speed == 2f && !it.requestedPlay }
        holdSpeed("⬇ 拖动到此处退出倍速")
        awaitPlayback { it.speed == 1f && !it.requestedPlay }
        back()
        awaitPoster(portrait)
        val next = onActivity { activity ->
            ViewModelProvider(activity)[LibraryViewModel::class.java].detailPosition(portrait.id).first == 0
        }
        compose.onNodeWithContentDescription("视频播放区域").performTouchInput {
            if (next) swipeLeft(durationMillis = 330L) else swipeRight(durationMillis = 330L)
        }
        awaitPoster(landscape)
        awaitServer {
            graph.media.asset(portrait.id).let { it.positionVersion == portrait.version && it.positionMs == stoppedVertical.position }
        }
        assertTrue(runBlocking { graph.media.history().items.any { it.assetId == portrait.id && it.contentVersion == portrait.version } })
    }

    private suspend fun uploadSample(libraryId: String, fileName: String): Asset {
        val uri = Uri.parse("content://media.qimeng.next.test.fixture/$fileName")
        copyCloudVideo(uri)
        val input = graph.uploads.receive(listOf(uri)).single()
        graph.uploads.enqueue(libraryId, "", UploadMode.ORDINARY, inputIds = listOf(input))
        val transfer = withTimeout(90_000L) {
            var current = graph.queue.state.value!!.transfers.single { it.id == input }
            while (current.state != TransferState.SUCCEEDED) {
                check(current.state != TransferState.FAILED) { current.message }
                delay(350L)
                current = graph.queue.state.value!!.transfers.single { it.id == input }
            }
            current
        }
        return graph.media.asset(requireNotNull(transfer.assetId))
    }

    private fun seed(): Triple<Asset, Asset, String> = runBlocking {
        val library = graph.media.createLibrary("云端视频交互样本", false)
        val author = graph.media.createAuthor(AUTHOR_NAME)
        val horizontal = uploadSample(library.id, "parity-landscape.mp4")
        val vertical = uploadSample(library.id, "parity-portrait.mp4")
        for (sample in listOf(horizontal, vertical)) {
            graph.media.updateAuthors(sample, listOf(author.id), emptyList(), sample.revision, sample.authorsRevision)
        }
        Triple(horizontal, vertical, author.id)
    }

    private fun openFromGrid(key: String, asset: Asset) {
        val board = hasTestTag("media-browser:$key")
        val grid = hasScrollAction() and hasAnyAncestor(board)
        val card = hasContentDescription(asset.name) and hasClickAction()
        compose.waitUntil(30_000) { compose.onAllNodes(grid).fetchSemanticsNodes().size == 1 }
        compose.onNode(grid).performScrollToNode(card)
        compose.onNode(card).assertIsDisplayed().performTouchInput { click(center) }
        awaitPoster(asset)
    }

    private fun awaitPoster(asset: Asset) {
        compose.waitUntil(20_000) {
            onActivity { activity -> ViewModelProvider(activity)[LibraryViewModel::class.java].asset.value?.id == asset.id } &&
                compose.onAllNodesWithContentDescription("播放视频", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
        }
        assertNull("The poster must have no attached output", sample())
    }

    private fun startPlayback() {
        compose.onNodeWithContentDescription("视频播放区域").performTouchInput { click(center) }
    }

    private fun awaitServer(condition: suspend () -> Boolean) = runBlocking {
        withTimeout(25_000L) { while (!condition()) delay(250L) }
    }

    private fun back() = onActivity { it.onBackPressedDispatcher.onBackPressed() }

    private fun tap(description: String) {
        var bounds: Rect? = null
        compose.waitUntil(8_000) {
            bounds = controlBounds(description)
            bounds != null
        }
        click(requireNotNull(bounds))
    }

    private fun controlBounds(description: String, enabled: Boolean = true): Rect? = onActivity { activity ->
        activity.findVideoControl(description)?.takeIf { it.isEnabled == enabled }?.screenBounds()?.takeUnless(Rect::isEmpty)
    }

    private fun tapSurface(twice: Boolean = false) {
        val bounds = onActivity { requireNotNull(it.findViewById<PlayerView>(R.id.video_playback_window)).screenBounds() }
        click(bounds)
        if (twice) {
            SystemClock.sleep(65L)
            click(bounds)
        }
    }

    private fun holdSpeed(dropTarget: String? = null) {
        val screen = onActivity { requireNotNull(it.findViewById<PlayerView>(R.id.video_playback_window)).screenBounds() }
        val down = SystemClock.uptimeMillis()
        val startX = screen.exactCenterX()
        val startY = screen.exactCenterY()
        sendVideoTouch(down, MotionEvent.ACTION_DOWN, startX, startY)
        var endX = startX
        var endY = startY
        try {
            awaitPlayback { it.speed == 2f }
            if (dropTarget != null) {
                var bounds: Rect? = null
                compose.waitUntil(3_000) {
                    bounds = onActivity { activity ->
                        val labels = arrayListOf<View>()
                        activity.window.decorView.findViewsWithText(labels, dropTarget, View.FIND_VIEWS_WITH_TEXT)
                        labels.filterIsInstance<TextView>().singleOrNull { it.text == dropTarget && it.isShown }?.screenBounds()
                    }
                    bounds != null
                }
                val hint = requireNotNull(bounds)
                endX = hint.exactCenterX()
                endY = hint.exactCenterY()
                sendVideoTouch(down, MotionEvent.ACTION_MOVE, endX, endY)
            }
        } finally {
            sendVideoTouch(down, MotionEvent.ACTION_UP, endX, endY)
        }
    }

    private fun dragSeekBar(to: Float) {
        val bar = requireNotNull(controlBounds("播放进度"))
        drag(bar.left + bar.width() * 0.12f, bar.left + bar.width() * to, bar.exactCenterY())
    }

    private fun drag(from: Float, to: Float, y: Float, whileHolding: () -> Unit = {}) {
        val down = SystemClock.uptimeMillis()
        sendVideoTouch(down, MotionEvent.ACTION_DOWN, from, y)
        for (step in 1..35) {
            sendVideoTouch(down, MotionEvent.ACTION_MOVE, from + (to - from) * step / 35f, y)
        }
        try { whileHolding() }
        finally { sendVideoTouch(down, MotionEvent.ACTION_UP, to, y) }
    }

    private fun click(bounds: Rect) {
        val down = SystemClock.uptimeMillis()
        sendVideoTouch(down, MotionEvent.ACTION_DOWN, bounds.exactCenterX(), bounds.exactCenterY())
        sendVideoTouch(down, MotionEvent.ACTION_UP, bounds.exactCenterX(), bounds.exactCenterY())
    }

    private fun waitForControls(visible: Boolean) {
        compose.waitUntil(9_000) {
            onActivity { activity ->
                val insets = ViewCompat.getRootWindowInsets(activity.window.decorView) ?: return@onActivity false
                (activity.findVideoControl("播放进度") != null) == visible &&
                    insets.isVisible(WindowInsetsCompat.Type.statusBars()) == visible &&
                    insets.isVisible(WindowInsetsCompat.Type.navigationBars()) == visible
            }
        }
    }

    private fun assertFullscreenInsets() = onActivity { activity ->
        val window = activity.window.decorView.screenBounds()
        val insets = requireNotNull(ViewCompat.getRootWindowInsets(activity.window.decorView))
        val statusTop = insets.getInsets(WindowInsetsCompat.Type.statusBars()).top
        val navigation = insets.getInsets(WindowInsetsCompat.Type.navigationBars())
        val exit = requireNotNull(activity.findVideoControl("退出全屏")).screenBounds()
        val controls = requireNotNull(activity.findVideoControl("播放进度")).parent.parent as View
        val bar = controls.screenBounds()
        assertTrue("The independent exit clears the status bar", exit.top >= window.top + statusTop)
        assertTrue("The bottom row is padded above the navigation bar", controls.paddingBottom > navigation.bottom)
        assertTrue("Playback controls remain within the visible window", bar.bottom <= window.bottom)
        assertTrue(activity.findVideoControl("退出横屏")?.screenBounds()?.right?.let { it <= window.right - navigation.right } == true)
    }

    private fun captureFrame(name: String, landscape: Boolean) {
        compose.waitUntil(12_000) {
            captureCloudScreenshot(name) { bitmap ->
                if ((bitmap.width > bitmap.height) != landscape) return@captureCloudScreenshot false
                val readable = if (bitmap.config == Bitmap.Config.HARDWARE) requireNotNull(bitmap.copy(Bitmap.Config.ARGB_8888, false)) else bitmap
                try { saturatedSamples(readable) >= 30 }
                finally { if (readable !== bitmap) readable.recycle() }
            }
        }
    }

    private fun awaitPlayback(condition: (VideoSample) -> Boolean): VideoSample {
        var finished: VideoSample? = null
        compose.waitUntil(12_000) {
            finished = sample()?.takeIf(condition)
            finished != null
        }
        return requireNotNull(finished)
    }

    private fun sample(): VideoSample? = onActivity { activity ->
        val view = activity.findViewById<PlayerView>(R.id.video_playback_window) ?: return@onActivity null
        val player = view.player as? ExoPlayer ?: return@onActivity null
        val surface = view.videoSurfaceView as? SurfaceView ?: return@onActivity null
        VideoSample(view, player, surface, player.currentPosition, player.playWhenReady, player.isPlaying,
            player.volume, player.playbackParameters.speed, player.playbackState, player.videoSize.width, player.videoSize.height,
            activity.requestedOrientation, activity.resources.configuration.orientation)
    }

    private fun <T> onActivity(read: (MainActivity) -> T): T {
        var result: Result<T>? = null
        compose.activityRule.scenario.onActivity { result = runCatching { read(it) } }
        return requireNotNull(result).getOrThrow()
    }

    private data class VideoSample(val view: PlayerView, val player: ExoPlayer, val surface: SurfaceView,
        val position: Long, val requestedPlay: Boolean, val playing: Boolean, val volume: Float, val speed: Float,
        val state: Int, val width: Int, val height: Int, val orientationRequest: Int, val orientation: Int)

    private companion object {
        const val AUTHOR_NAME = "云端视频合成作者"
    }
}

internal fun Activity.findVideoControl(description: String): View? {
    val views = arrayListOf<View>()
    window.decorView.findViewsWithText(views, description, View.FIND_VIEWS_WITH_CONTENT_DESCRIPTION)
    return views.singleOrNull { it.isShown && it.contentDescription == description }
}

internal fun View.screenBounds(): Rect {
    val location = IntArray(2)
    getLocationOnScreen(location)
    return Rect(location[0], location[1], location[0] + width, location[1] + height)
}

internal fun sendVideoTouch(downTime: Long, action: Int, x: Float, y: Float) {
    val event = MotionEvent.obtain(downTime, SystemClock.uptimeMillis(), action, x, y, 0)
    try { InstrumentationRegistry.getInstrumentation().sendPointerSync(event) }
    finally { event.recycle() }
}
