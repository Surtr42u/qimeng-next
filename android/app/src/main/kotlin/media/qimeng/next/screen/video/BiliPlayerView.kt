package media.qimeng.next.screen.video

import android.content.Context
import android.graphics.Color
import android.graphics.PorterDuff
import android.graphics.PorterDuffColorFilter
import android.graphics.Rect
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.view.GestureDetector
import android.view.Gravity
import android.view.LayoutInflater
import android.view.MotionEvent
import android.view.View
import android.widget.FrameLayout
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.PopupWindow
import android.widget.SeekBar
import android.widget.TextView
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.isVisible
import androidx.media3.common.Player
import androidx.media3.common.VideoSize
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView
import media.qimeng.next.R
import kotlin.math.abs

/** Old BiliPlayerView's two-row controls, popups and touch overlay on Next's single Media3 output. */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
internal class BiliPlayerView(context: Context) : FrameLayout(context) {
    private val playerView = (LayoutInflater.from(context).inflate(R.layout.video_player, this, false) as PlayerView).apply {
        useController = false
        setShowBuffering(PlayerView.SHOW_BUFFERING_WHEN_PLAYING)
    }
    private val bottomBar: LinearLayout
    private val playPauseBtn: ImageView
    private val qualityBtn: TextView
    private val speedBtn: TextView
    private val muteBtn: ImageView
    private val fullscreenBtn: ImageView
    private val fullscreenExit: ImageView
    private val currentTimeText: TextView
    private val totalTimeText: TextView
    private val progressBar: SeekBar

    private val gestureIndicator: LinearLayout
    private val gestureText: TextView
    private val topIndicator: LinearLayout
    private val topIndicatorText: TextView
    private val lockSpeedHint: LinearLayout
    private val lockSpeedHintText: TextView

    var onFullscreen: ((Boolean) -> Unit)? = null
    var onControllerVisibilityChanged: ((Boolean) -> Unit)? = null

    private var isFullscreen = false
    private var controllerVisible = false
    private var currentSpeed = 1f
    private var currentAspectRatio = PlayerAspectRatio.DEFAULT
    private var isMuted = true
    private var gestureStartX = 0f
    private var gestureStartY = 0f
    private var seekStartPosition: Long? = null
    private var seekPreview: Long? = null
    private var isSeekbarDragging = false
    private var seekbarCancelled = false
    private var isLongPressing = false
    private var speedBeforeLongPress = 1f
    private var isSpeedLocked = false
    private var isLockHintHovered = false
    private var popup: PopupWindow? = null

    private val duration: Long get() = playerView.player?.duration?.coerceAtLeast(0L) ?: 0L

    private val playerListener = object : Player.Listener {
        override fun onIsPlayingChanged(isPlaying: Boolean) {
            updatePlayPauseButton()
            resetHideTimer()
        }

        override fun onPlayWhenReadyChanged(playWhenReady: Boolean, reason: Int) {
            updatePlayPauseButton()
            if (!playWhenReady) showController(true)
        }

        override fun onPlaybackStateChanged(playbackState: Int) {
            updatePlayPauseButton()
            syncPosition()
            if (playbackState == Player.STATE_READY) updateQualityButtonText()
            if (playbackState == Player.STATE_ENDED) showController(true)
        }

        override fun onVideoSizeChanged(videoSize: VideoSize) {
            applyAspectRatio(currentAspectRatio)
            updateQualityButtonText()
        }
    }
    private val hideController = Runnable { if (playerView.player?.isPlaying == true) showController(false) }
    private val hideTopIndicator = Runnable { topIndicator.isVisible = false }
    private val updateProgress = object : Runnable {
        override fun run() {
            syncPosition()
            postDelayed(this, 200L)
        }
    }
    private val gestureDetector = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
        override fun onDown(e: MotionEvent) = true

        override fun onSingleTapConfirmed(e: MotionEvent) = performClick()

        override fun onDoubleTap(e: MotionEvent): Boolean {
            togglePlayPause()
            return true
        }

        override fun onLongPress(e: MotionEvent) {
            val player = playerView.player ?: return
            isLongPressing = true
            speedBeforeLongPress = currentSpeed
            if (isSpeedLocked) {
                showTopIndicator("长按拖动退出倍速")
                lockSpeedHintText.text = "⬇ 拖动到此处退出倍速"
            } else {
                player.setPlaybackSpeed(LONG_PRESS_SPEED)
                currentSpeed = LONG_PRESS_SPEED
                updateSpeedButtonText()
                showTopIndicator("长按倍速 ${LONG_PRESS_SPEED}x")
                lockSpeedHintText.text = "⬇ 拖动到此处锁定倍速"
            }
            if (isFullscreen) {
                showController(false)
            } else {
                lockSpeedHint.isVisible = true
                showController(true)
            }
            removeCallbacks(hideController)
        }
    })

    init {
        setBackgroundColor(Color.BLACK)
        isClickable = true
        contentDescription = "视频播放区域"
        addView(playerView)

        gestureIndicator = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER
            setPadding(24.dp(context), 20.dp(context), 24.dp(context), 20.dp(context))
            setBackgroundColor(0xB3000000.toInt())
            visibility = View.GONE
            layoutParams = LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT).apply {
                gravity = Gravity.CENTER
            }
        }
        gestureText = playerText("", 14f).apply { gravity = Gravity.CENTER }
        gestureIndicator.addView(gestureText)
        addView(gestureIndicator)

        topIndicator = LinearLayout(context).apply {
            gravity = Gravity.CENTER
            setPadding(16.dp(context), 8.dp(context), 16.dp(context), 8.dp(context))
            setBackgroundColor(0x66000000)
            visibility = View.GONE
            layoutParams = LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT).apply {
                gravity = Gravity.TOP or Gravity.CENTER_HORIZONTAL
                topMargin = 60.dp(context)
            }
        }
        topIndicatorText = playerText("", 13f).apply { gravity = Gravity.CENTER }
        topIndicator.addView(topIndicatorText)
        addView(topIndicator)

        lockSpeedHint = LinearLayout(context).apply {
            gravity = Gravity.CENTER
            setPadding(16.dp(context), 10.dp(context), 16.dp(context), 10.dp(context))
            setBackgroundColor(0x66000000)
            visibility = View.GONE
            layoutParams = LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT).apply {
                gravity = Gravity.BOTTOM or Gravity.CENTER_HORIZONTAL
                bottomMargin = 100.dp(context)
            }
        }
        lockSpeedHintText = playerText("", 13f).apply { gravity = Gravity.CENTER }
        lockSpeedHint.addView(lockSpeedHintText)
        addView(lockSpeedHint)

        fullscreenExit = ImageView(context).apply {
            setImageResource(R.drawable.ic_player_fullscreen_exit)
            setColorFilter(Color.WHITE)
            contentDescription = "退出全屏"
            setPadding(10.dp(context), 10.dp(context), 10.dp(context), 10.dp(context))
            background = GradientDrawable().apply {
                setColor(0x88000000.toInt())
                cornerRadius = 10.dp(context).toFloat()
            }
            visibility = View.GONE
            layoutParams = LayoutParams(44.dp(context), 44.dp(context)).apply {
                gravity = Gravity.TOP or Gravity.END
                topMargin = 12.dp(context)
                marginEnd = 14.dp(context)
            }
            setOnClickListener { onFullscreen?.invoke(false) }
        }
        addView(fullscreenExit)

        bottomBar = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(16.dp(context), 8.dp(context), 16.dp(context), 12.dp(context))
            setBackgroundColor(0x88000000.toInt())
            visibility = View.GONE
            layoutParams = LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT).apply {
                gravity = Gravity.BOTTOM
            }
        }

        val progressRow = LinearLayout(context).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
        }
        currentTimeText = playerText("00:00", 12f)
        totalTimeText = playerText("--:--", 12f)
        progressBar = SeekBar(context).apply {
            contentDescription = "播放进度"
            layoutParams = LinearLayout.LayoutParams(0, LayoutParams.WRAP_CONTENT, 1f).apply {
                marginStart = 8.dp(context)
                marginEnd = 8.dp(context)
            }
            max = SEEK_STEPS
            progressDrawable?.colorFilter = PorterDuffColorFilter(Color.WHITE, PorterDuff.Mode.SRC_IN)
            thumb?.colorFilter = PorterDuffColorFilter(Color.WHITE, PorterDuff.Mode.SRC_IN)
            setOnTouchListener { _, event ->
                if (event.actionMasked == MotionEvent.ACTION_CANCEL) seekbarCancelled = true
                false
            }
            setOnSeekBarChangeListener(object : SeekBar.OnSeekBarChangeListener {
                override fun onProgressChanged(seekBar: SeekBar, progress: Int, fromUser: Boolean) {
                    val length = duration
                    if (!fromUser || length == 0L) return
                    val preview = progress.toLong() * length / SEEK_STEPS
                    currentTimeText.text = formatDurationMs(preview)
                    if (!isSeekbarDragging) playerView.player?.seekTo(preview)
                }

                override fun onStartTrackingTouch(seekBar: SeekBar) {
                    isSeekbarDragging = true
                    seekbarCancelled = false
                    removeCallbacks(hideController)
                }

                override fun onStopTrackingTouch(seekBar: SeekBar) {
                    isSeekbarDragging = false
                    val length = duration
                    if (length > 0L && !seekbarCancelled) {
                        playerView.player?.seekTo(seekBar.progress.toLong() * length / SEEK_STEPS)
                    }
                    syncPosition()
                    resetHideTimer()
                }
            })
        }
        progressRow.addView(currentTimeText)
        progressRow.addView(progressBar)
        progressRow.addView(totalTimeText)
        bottomBar.addView(progressRow)

        val buttonRow = LinearLayout(context).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            setPadding(0, 4.dp(context), 0, 0)
        }
        playPauseBtn = controlButton(R.drawable.ic_player_play, "播放", ::togglePlayPause)
        muteBtn = controlButton(R.drawable.ic_player_mute, "开启声音", ::toggleMute)
        qualityBtn = pillButton("原画", 6, ::showQualityPopup)
        speedBtn = pillButton("倍速", 4, ::showSpeedPopup)
        val settingsBtn = controlButton(R.drawable.ic_player_settings, "播放设置", ::showSettingsPopup).apply {
            (layoutParams as LinearLayout.LayoutParams).apply {
                marginStart = 2.dp(context)
                marginEnd = 2.dp(context)
            }
        }
        fullscreenBtn = controlButton(R.drawable.ic_player_fullscreen, "进入全屏") {
            onFullscreen?.invoke(!isFullscreen)
        }
        buttonRow.addView(playPauseBtn)
        buttonRow.addView(muteBtn)
        buttonRow.addView(View(context), LinearLayout.LayoutParams(0, 1, 1f))
        buttonRow.addView(qualityBtn)
        buttonRow.addView(speedBtn)
        buttonRow.addView(settingsBtn)
        buttonRow.addView(fullscreenBtn)
        bottomBar.addView(buttonRow)
        addView(bottomBar)
        ViewCompat.setOnApplyWindowInsetsListener(this) { _, insets ->
            applyInsets(insets)
            insets
        }
    }

    fun setPlayer(player: ExoPlayer) {
        playerView.player = player
        isMuted = player.volume == 0f
        currentSpeed = player.playbackParameters.speed
        isSpeedLocked = currentSpeed != 1f
        updateMuteButton()
        updateSpeedButtonText()
        updateQualityButtonText()
        updatePlayPauseButton()
        syncPosition()
        if (isAttachedToWindow) observePlayer()
        showController(true)
    }

    fun detachPlayer() {
        detachControls()
        playerView.player = null
        onFullscreen = null
        onControllerVisibilityChanged = null
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        if (playerView.player != null) observePlayer()
        ViewCompat.requestApplyInsets(this)
    }

    override fun onDetachedFromWindow() {
        detachControls()
        super.onDetachedFromWindow()
    }

    private fun observePlayer() {
        playerView.player?.addListener(playerListener)
        post(updateProgress)
        resetHideTimer()
    }

    private fun detachControls() {
        if (seekStartPosition != null) finishSeek(commit = false)
        if (isLongPressing) finishLongPress(commit = false)
        playerView.player?.removeListener(playerListener)
        popup?.dismiss()
        removeCallbacks(updateProgress)
        removeCallbacks(hideController)
        removeCallbacks(hideTopIndicator)
    }

    fun setFullscreen(fullscreen: Boolean, available: Boolean) {
        fullscreenBtn.isEnabled = available || fullscreen
        if (fullscreen == isFullscreen) return
        isFullscreen = fullscreen
        fullscreenExit.isVisible = fullscreen
        fullscreenBtn.contentDescription = if (fullscreen) "退出横屏" else "进入全屏"
        fullscreenBtn.setImageResource(if (fullscreen) R.drawable.ic_player_fullscreen_exit else R.drawable.ic_player_fullscreen)
        popup?.dismiss()
        showController(!fullscreen || playerView.player?.isPlaying != true)
        ViewCompat.requestApplyInsets(this)
    }

    private fun applyInsets(insets: WindowInsetsCompat) {
        val bottomInsets = insets.getInsets(WindowInsetsCompat.Type.navigationBars() or WindowInsetsCompat.Type.displayCutout())
        bottomBar.setPadding(
            16.dp(context) + bottomInsets.left, 8.dp(context),
            16.dp(context) + bottomInsets.right, 12.dp(context) + bottomInsets.bottom,
        )
        val topInsets = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
        val exitParams = fullscreenExit.layoutParams as LayoutParams
        val endInset = if (layoutDirection == View.LAYOUT_DIRECTION_RTL) topInsets.left else topInsets.right
        val topMargin = 12.dp(context) + topInsets.top
        val endMargin = 14.dp(context) + endInset
        if (exitParams.topMargin != topMargin || exitParams.marginEnd != endMargin) {
            exitParams.topMargin = topMargin
            exitParams.marginEnd = endMargin
            fullscreenExit.layoutParams = exitParams
        }
    }

    private fun syncPosition() {
        val player = playerView.player ?: return
        val length = duration
        totalTimeText.text = if (length > 0L) formatDurationMs(length) else "--:--"
        if (isSeekbarDragging || seekPreview != null) return
        val position = player.currentPosition
        currentTimeText.text = formatDurationMs(position)
        progressBar.progress = if (length > 0L) (position * SEEK_STEPS / length).toInt() else 0
    }

    private fun togglePlayPause() {
        val player = playerView.player ?: return
        if (player.currentMediaItem == null) return
        if (player.playWhenReady && player.playbackState != Player.STATE_ENDED) {
            player.pause()
        } else {
            if (player.playbackState == Player.STATE_ENDED) player.seekTo(0L)
            player.play()
        }
        showController(true)
    }

    private fun updatePlayPauseButton() {
        val player = playerView.player ?: return
        val playing = player.playWhenReady && player.playbackState != Player.STATE_ENDED
        playPauseBtn.contentDescription = if (playing) "暂停" else "播放"
        playPauseBtn.setImageResource(if (playing) R.drawable.ic_player_pause else R.drawable.ic_player_play)
    }

    private fun updateMuteButton() {
        muteBtn.contentDescription = if (isMuted) "开启声音" else "静音"
        muteBtn.setImageResource(if (isMuted) R.drawable.ic_player_mute else R.drawable.ic_player_unmute)
    }

    private fun toggleMute() {
        val player = playerView.player as? ExoPlayer ?: return
        isMuted = !isMuted
        player.volume = if (isMuted) 0f else 1f
        updateMuteButton()
        showTopIndicator(if (isMuted) "🔇 已静音" else "🔊 已开启声音")
        showController(true)
    }

    private fun updateQualityButtonText() {
        qualityBtn.text = qualityLabelText(playerView.player?.videoSize?.height ?: 0)
    }

    private fun playerText(label: String, size: Float) = TextView(context).apply {
        text = label
        setTextColor(Color.WHITE)
        textSize = size
    }

    private fun controlButton(icon: Int, label: String, onClick: () -> Unit) = ImageView(context).apply {
        setImageResource(icon)
        setPadding(4.dp(context), 4.dp(context), 4.dp(context), 4.dp(context))
        layoutParams = LinearLayout.LayoutParams(36.dp(context), 36.dp(context))
        setColorFilter(Color.WHITE)
        contentDescription = label
        setOnClickListener { onClick() }
    }

    private fun pillButton(label: String, startMargin: Int, onClick: () -> Unit) = playerText(label, 12f).apply {
        typeface = Typeface.create(Typeface.DEFAULT, Typeface.BOLD)
        gravity = Gravity.CENTER
        background = GradientDrawable().apply {
            cornerRadius = 14.dp(context).toFloat()
            setColor(0x2EFFFFFF.toInt())
            setStroke((0.8f * context.resources.displayMetrics.density).toInt().coerceAtLeast(1), 0x4DFFFFFF.toInt())
        }
        setPadding(10.dp(context), 4.dp(context), 10.dp(context), 4.dp(context))
        layoutParams = LinearLayout.LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT).apply {
            marginStart = startMargin.dp(context)
            marginEnd = 2.dp(context)
        }
        setOnClickListener { onClick() }
    }

    private fun showQualityPopup() {
        val size = playerView.player?.videoSize
        val w = size?.width ?: 0
        val h = size?.height ?: 0
        val qualityDesc = if (w > 0 && h > 0) {
            "${w}×${h} · ${qualityLabelText(h)} · 原始画质"
        } else {
            "原始画质 · 纯净直出"
        }

        val container = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            val bg = GradientDrawable().apply {
                cornerRadius = 14.dp(context).toFloat()
                setColor(0xEE1E1E1E.toInt())
                setStroke(1, 0x33FFFFFF.toInt())
            }
            background = bg
            setPadding(12.dp(context), 10.dp(context), 12.dp(context), 10.dp(context))
        }

        val titleView = TextView(context).apply {
            text = "清晰度"
            setTextColor(0x99FFFFFF.toInt())
            textSize = 12f
            setPadding(6.dp(context), 2.dp(context), 6.dp(context), 8.dp(context))
        }
        container.addView(titleView)

        val item = LinearLayout(context).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            val itemBg = GradientDrawable().apply {
                cornerRadius = 10.dp(context).toFloat()
                setColor(0x2E4FC3F7.toInt())
                setStroke(1, 0x804FC3F7.toInt())
            }
            background = itemBg
            setPadding(12.dp(context), 10.dp(context), 14.dp(context), 10.dp(context))
            setOnClickListener {
                popup?.dismiss()
                showController(true)
            }
        }

        val textLayout = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
        }
        val mainText = TextView(context).apply {
            text = "原画"
            setTextColor(SPEED_SELECTED_COLOR)
            textSize = 14f
            typeface = Typeface.create(Typeface.DEFAULT, Typeface.BOLD)
        }
        val subText = TextView(context).apply {
            text = qualityDesc
            setTextColor(0xCCFFFFFF.toInt())
            textSize = 11f
        }
        textLayout.addView(mainText)
        textLayout.addView(subText)

        val checkMark = TextView(context).apply {
            text = "✓"
            setTextColor(SPEED_SELECTED_COLOR)
            textSize = 14f
            typeface = Typeface.create(Typeface.DEFAULT, Typeface.BOLD)
            setPadding(8.dp(context), 0, 0, 0)
        }
        item.addView(textLayout)
        item.addView(checkMark)
        container.addView(item)
        showPopup(container, qualityBtn)
    }

    private fun showSpeedPopup() {
        val container = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            val bg = GradientDrawable().apply {
                cornerRadius = 14.dp(context).toFloat()
                setColor(0xEE1E1E1E.toInt())
                setStroke(1, 0x33FFFFFF.toInt())
            }
            background = bg
            setPadding(6.dp(context), 6.dp(context), 6.dp(context), 6.dp(context))
        }
        for (tier in PLAYER_SPEED_TIERS) {
            val isSelected = abs(currentSpeed - tier.speed) < 0.01f
            val item = TextView(context).apply {
                text = tier.menuLabel
                setTextColor(if (isSelected) SPEED_SELECTED_COLOR else Color.WHITE)
                textSize = 13f
                typeface = if (isSelected) Typeface.create(Typeface.DEFAULT, Typeface.BOLD) else Typeface.DEFAULT
                gravity = Gravity.CENTER
                val itemBg = GradientDrawable().apply {
                    cornerRadius = 8.dp(context).toFloat()
                    if (isSelected) {
                        setColor(0x2E4FC3F7.toInt())
                    } else {
                        setColor(Color.TRANSPARENT)
                    }
                }
                background = itemBg
                setPadding(20.dp(context), 8.dp(context), 20.dp(context), 8.dp(context))
                setOnClickListener {
                    currentSpeed = tier.speed
                    isSpeedLocked = currentSpeed != 1f
                    playerView.player?.setPlaybackSpeed(currentSpeed)
                    updateSpeedButtonText()
                    popup?.dismiss()
                    showController(true)
                }
            }
            container.addView(item)
        }
        showPopup(container, speedBtn)
    }

    private fun showSettingsPopup() {
        val container = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            val bg = GradientDrawable().apply {
                cornerRadius = 14.dp(context).toFloat()
                setColor(0xEE1E1E1E.toInt())
                setStroke(1, 0x33FFFFFF.toInt())
            }
            background = bg
            setPadding(16.dp(context), 14.dp(context), 16.dp(context), 14.dp(context))
        }

        // 标题
        val titleView = TextView(context).apply {
            text = "播放设置"
            setTextColor(Color.WHITE)
            textSize = 14f
            typeface = Typeface.create(Typeface.DEFAULT, Typeface.BOLD)
            setPadding(0, 0, 0, 8.dp(context))
        }
        container.addView(titleView)

        // 分割线
        val divider1 = View(context).apply {
            setBackgroundColor(0x26FFFFFF.toInt())
            layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 1.dp(context)).apply {
                bottomMargin = 10.dp(context)
            }
        }
        container.addView(divider1)

        // 1. 画面比例
        val ratioLabel = TextView(context).apply {
            text = "画面比例"
            setTextColor(0x99FFFFFF.toInt())
            textSize = 12f
            setPadding(0, 0, 0, 6.dp(context))
        }
        container.addView(ratioLabel)

        val ratioRow = LinearLayout(context).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT).apply {
                bottomMargin = 12.dp(context)
            }
        }
        for (ratio in PlayerAspectRatio.entries) {
            val isSelected = currentAspectRatio == ratio
            val pill = TextView(context).apply {
                text = ratio.label
                setTextColor(if (isSelected) SPEED_SELECTED_COLOR else Color.WHITE)
                textSize = 12f
                typeface = if (isSelected) Typeface.create(Typeface.DEFAULT, Typeface.BOLD) else Typeface.DEFAULT
                gravity = Gravity.CENTER
                val pillBg = GradientDrawable().apply {
                    cornerRadius = 8.dp(context).toFloat()
                    if (isSelected) {
                        setColor(0x2E4FC3F7.toInt())
                        setStroke(1, 0x804FC3F7.toInt())
                    } else {
                        setColor(0x22FFFFFF.toInt())
                    }
                }
                background = pillBg
                setPadding(10.dp(context), 6.dp(context), 10.dp(context), 6.dp(context))
                layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f).apply {
                    marginEnd = if (ratio != PlayerAspectRatio.entries.last()) 6.dp(context) else 0
                }
                setOnClickListener {
                    currentAspectRatio = ratio
                    applyAspectRatio(ratio)
                    showTopIndicator("画面比例：${ratio.label}")
                    popup?.dismiss()
                    showController(true)
                }
            }
            ratioRow.addView(pill)
        }
        container.addView(ratioRow)

        // 分割线
        val divider2 = View(context).apply {
            setBackgroundColor(0x26FFFFFF.toInt())
            layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 1.dp(context)).apply {
                bottomMargin = 10.dp(context)
            }
        }
        container.addView(divider2)

        // 2. 单片循环
        val loopRow = LinearLayout(context).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT)
        }
        val loopText = TextView(context).apply {
            text = "单片循环"
            setTextColor(Color.WHITE)
            textSize = 13f
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
        }
        val isLooping = playerView.player?.repeatMode == Player.REPEAT_MODE_ONE
        val loopSwitch = TextView(context).apply {
            text = if (isLooping) "已开启" else "已关闭"
            setTextColor(if (isLooping) SPEED_SELECTED_COLOR else 0xAAFFFFFF.toInt())
            textSize = 12f
            typeface = Typeface.create(Typeface.DEFAULT, Typeface.BOLD)
            val pillBg = GradientDrawable().apply {
                cornerRadius = 10.dp(context).toFloat()
                if (isLooping) {
                    setColor(0x2E4FC3F7.toInt())
                    setStroke(1, 0x804FC3F7.toInt())
                } else {
                    setColor(0x22FFFFFF.toInt())
                }
            }
            background = pillBg
            setPadding(12.dp(context), 6.dp(context), 12.dp(context), 6.dp(context))
            setOnClickListener {
                val nextLoop = playerView.player?.repeatMode != Player.REPEAT_MODE_ONE
                playerView.player?.repeatMode = if (nextLoop) Player.REPEAT_MODE_ONE else Player.REPEAT_MODE_OFF
                showTopIndicator(if (nextLoop) "单片循环：开启" else "单片循环：关闭")
                popup?.dismiss()
                showController(true)
            }
        }
        loopRow.addView(loopText)
        loopRow.addView(loopSwitch)
        container.addView(loopRow)
        showPopup(container)
    }

    private fun showPopup(container: LinearLayout, anchor: View? = null) {
        popup?.dismiss()
        removeCallbacks(hideController)
        val popupWidth = if (anchor == null) 270.dp(context) else LayoutParams.WRAP_CONTENT
        val window = PopupWindow(container, popupWidth, LayoutParams.WRAP_CONTENT).apply {
            isFocusable = true
            isOutsideTouchable = true
            setBackgroundDrawable(null)
            elevation = 8.dp(context).toFloat()
            setOnDismissListener {
                popup = null
                resetHideTimer()
            }
        }
        popup = window
        if (anchor == null) {
            window.showAtLocation(this, Gravity.BOTTOM or Gravity.CENTER_HORIZONTAL, 0, bottomBar.height + 14.dp(context))
        } else {
            container.measure(View.MeasureSpec.UNSPECIFIED, View.MeasureSpec.UNSPECIFIED)
            val xOffset = -(container.measuredWidth - anchor.width) / 2
            window.showAsDropDown(anchor, xOffset, -(anchor.height + container.measuredHeight + 6.dp(context)))
        }
    }

    private fun applyAspectRatio(ratio: PlayerAspectRatio) {
        val contentFrame = playerView.findViewById<AspectRatioFrameLayout>(androidx.media3.ui.R.id.exo_content_frame)
        val size = playerView.player?.videoSize
        val aspect = when (ratio) {
            PlayerAspectRatio.DEFAULT -> if (size != null && size.height > 0) {
                size.width * size.pixelWidthHeightRatio / size.height
            } else 0f
            PlayerAspectRatio.RATIO_16_9 -> 16f / 9f
            PlayerAspectRatio.RATIO_4_3 -> 4f / 3f
            PlayerAspectRatio.FILL -> 0f
        }
        playerView.resizeMode = if (ratio == PlayerAspectRatio.FILL) {
            AspectRatioFrameLayout.RESIZE_MODE_FILL
        } else AspectRatioFrameLayout.RESIZE_MODE_FIT
        contentFrame.setAspectRatio(aspect)
    }

    private fun updateSpeedButtonText() {
        speedBtn.text = if (currentSpeed == 1f) "倍速" else "${currentSpeed}x"
    }

    private fun showController(visible: Boolean) {
        controllerVisible = visible
        bottomBar.isVisible = visible
        onControllerVisibilityChanged?.invoke(visible)
        if (!visible) popup?.dismiss()
        resetHideTimer()
    }

    private fun resetHideTimer() {
        removeCallbacks(hideController)
        if (isAttachedToWindow && controllerVisible && playerView.player?.isPlaying == true &&
            !isSeekbarDragging && !isLongPressing && popup == null
        ) {
            postDelayed(hideController, HIDE_DELAY_MS)
        }
    }

    private fun showTopIndicator(text: String) {
        topIndicatorText.text = text
        topIndicator.isVisible = true
        removeCallbacks(hideTopIndicator)
        postDelayed(hideTopIndicator, 1_500L)
    }

    override fun performClick(): Boolean {
        if (playerView.player == null) return false
        super.performClick()
        showController(!controllerVisible)
        return true
    }

    override fun onTouchEvent(event: MotionEvent): Boolean {
        if (playerView.player == null) return false
        gestureDetector.onTouchEvent(event)
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                gestureStartX = event.x
                gestureStartY = event.y
                seekStartPosition = null
                seekPreview = null
            }
            MotionEvent.ACTION_MOVE -> {
                if (isLongPressing) updateLockHover(event) else previewSeek(event)
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                val commit = event.actionMasked == MotionEvent.ACTION_UP
                if (isLongPressing) {
                    if (commit) updateLockHover(event)
                    finishLongPress(commit)
                }
                if (seekStartPosition != null) finishSeek(commit)
            }
        }
        return true
    }

    private fun previewSeek(event: MotionEvent) {
        val dx = event.x - gestureStartX
        val dy = event.y - gestureStartY
        val length = duration
        if (seekStartPosition == null) {
            if (abs(dx) <= 24.dp(context) || abs(dx) <= abs(dy) || length <= 0L) return
            seekStartPosition = playerView.player?.currentPosition ?: return
            showController(false)
        }
        if (length <= 0L) return
        val delta = (dx / width * gestureSeekCapMs(length)).toLong()
        val position = (requireNotNull(seekStartPosition) + delta).coerceIn(0L, length)
        seekPreview = position
        val sign = if (delta >= 0L) "+" else ""
        gestureText.text = "$sign${formatDurationMs(delta)}  ${formatDurationMs(position)} / ${formatDurationMs(length)}"
        gestureIndicator.isVisible = true
        progressBar.progress = (position * SEEK_STEPS / length).toInt()
        currentTimeText.text = formatDurationMs(position)
        bottomBar.isVisible = true
    }

    private fun finishSeek(commit: Boolean) {
        if (commit) seekPreview?.let { playerView.player?.seekTo(it) }
        seekStartPosition = null
        seekPreview = null
        gestureIndicator.isVisible = false
        bottomBar.isVisible = controllerVisible
        syncPosition()
        resetHideTimer()
    }

    private fun updateLockHover(event: MotionEvent) {
        if (isFullscreen) return
        val rect = Rect()
        val hovered = lockSpeedHint.getGlobalVisibleRect(rect) && rect.contains(event.rawX.toInt(), event.rawY.toInt())
        if (hovered != isLockHintHovered) {
            isLockHintHovered = hovered
            lockSpeedHint.setBackgroundColor(if (hovered) 0xAAFFFFFF.toInt() else 0x66000000)
            lockSpeedHintText.setTextColor(if (hovered) Color.BLACK else Color.WHITE)
        }
    }

    private fun finishLongPress(commit: Boolean) {
        val droppedInHint = commit && isLockHintHovered && !isFullscreen
        val wasLocked = isSpeedLocked
        if (droppedInHint) {
            isSpeedLocked = !wasLocked
            currentSpeed = if (isSpeedLocked) LONG_PRESS_SPEED else 1f
            showTopIndicator(if (isSpeedLocked) "已锁定 ${LONG_PRESS_SPEED}x 倍速" else "已退出倍速锁定")
        } else {
            currentSpeed = speedBeforeLongPress
        }
        playerView.player?.setPlaybackSpeed(currentSpeed)
        updateSpeedButtonText()
        lockSpeedHint.isVisible = false
        lockSpeedHint.setBackgroundColor(0x66000000)
        lockSpeedHintText.setTextColor(Color.WHITE)
        isLockHintHovered = false
        isLongPressing = false
        if (!isFullscreen) showController(true)
    }

    override fun dispatchHoverEvent(event: MotionEvent): Boolean {
        if (isFullscreen && event.actionMasked == MotionEvent.ACTION_HOVER_MOVE &&
            event.getToolType(0) == MotionEvent.TOOL_TYPE_MOUSE && event.y >= height - 100.dp(context)
        ) {
            showController(true)
        }
        return super.dispatchHoverEvent(event)
    }

    private companion object {
        const val HIDE_DELAY_MS = 5_000L
        const val LONG_PRESS_SPEED = 2f
        const val SEEK_STEPS = 1_000
        const val SPEED_SELECTED_COLOR = 0xFF4FC3F7.toInt()
    }
}

private fun Int.dp(context: Context): Int = (this * context.resources.displayMetrics.density).toInt()
