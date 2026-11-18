package media.qimeng.next

import androidx.media3.common.Player

/** One player and content version own these events until their final checkpoint. */
internal class PlaybackHistory(
    private val player: Player,
    private val recordPlay: () -> Unit,
    private val recordProgress: (Long) -> Unit,
) : Player.Listener {
    private var hasPlayback = false
    private var lastPosition: Long? = null

    override fun onIsPlayingChanged(isPlaying: Boolean) {
        if (isPlaying) {
            hasPlayback = true
            recordPlay()
        }
    }

    override fun onPlayWhenReadyChanged(playWhenReady: Boolean, reason: Int) {
        if (!playWhenReady) checkpoint()
    }

    override fun onPlaybackStateChanged(playbackState: Int) {
        if (playbackState == Player.STATE_ENDED) checkpoint()
    }

    override fun onPositionDiscontinuity(oldPosition: Player.PositionInfo, newPosition: Player.PositionInfo, reason: Int) {
        if (reason == Player.DISCONTINUITY_REASON_SEEK && player.currentMediaItem != null) {
            hasPlayback = true
            save(newPosition.positionMs)
        }
    }

    fun checkpoint() {
        if (hasPlayback && player.currentMediaItem != null) save(player.currentPosition)
    }

    private fun save(position: Long) {
        if (position >= 0 && position != lastPosition) {
            lastPosition = position
            recordProgress(position)
        }
    }
}
