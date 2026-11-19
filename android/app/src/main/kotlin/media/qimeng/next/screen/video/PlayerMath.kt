package media.qimeng.next.screen.video

import java.util.Locale
import kotlin.math.abs

// From the installed app's feature/detail/video/PlayerMath.kt.
internal fun gestureSeekCapMs(durationMs: Long): Long = when {
    durationMs <= 30_000L -> durationMs
    durationMs <= 120_000L -> 60_000L
    else -> 120_000L
}

internal data class PlayerSpeedTier(val speed: Float, val menuLabel: String)

internal val PLAYER_SPEED_TIERS = listOf(
    PlayerSpeedTier(2.0f, "2.0x"),
    PlayerSpeedTier(1.5f, "1.5x"),
    PlayerSpeedTier(1.25f, "1.25x"),
    PlayerSpeedTier(1.0f, "1.0x 正常"),
    PlayerSpeedTier(0.75f, "0.75x"),
    PlayerSpeedTier(0.5f, "0.5x"),
)

internal fun qualityLabelText(height: Int): String = when {
    height >= 2160 -> "4K 超清"
    height >= 1440 -> "2K 超清"
    height >= 1080 -> "1080P 高清"
    height >= 720 -> "720P 高清"
    height >= 480 -> "480P 标清"
    height > 0 -> "${height}P"
    else -> "原画"
}

internal enum class PlayerAspectRatio(val label: String) {
    DEFAULT("默认"),
    RATIO_16_9("16:9"),
    RATIO_4_3("4:3"),
    FILL("拉伸铺满"),
}

internal fun formatDurationMs(ms: Long): String {
    val seconds = abs(ms) / 1_000
    val hours = seconds / 3_600
    val minutes = seconds / 60 % 60
    val remainder = seconds % 60
    val prefix = if (ms < 0L) "-" else ""
    return if (hours > 0L) {
        String.format(Locale.US, "%s%d:%02d:%02d", prefix, hours, minutes, remainder)
    } else {
        String.format(Locale.US, "%s%d:%02d", prefix, minutes, remainder)
    }
}
