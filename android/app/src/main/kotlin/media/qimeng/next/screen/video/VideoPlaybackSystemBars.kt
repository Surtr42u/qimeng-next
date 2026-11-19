package media.qimeng.next.screen.video

import android.os.Build
import android.os.CancellationSignal
import android.view.View
import android.view.Window
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.ui.platform.LocalConfiguration
import androidx.core.graphics.Insets
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsAnimationControlListenerCompat
import androidx.core.view.WindowInsetsAnimationControllerCompat
import androidx.core.view.WindowInsetsCompat

/** Playback uses the installed app's zero-duration hide so the system bars follow the control strip. */
@Composable
internal fun VideoPlaybackSystemBars(window: Window, view: View, visible: Boolean) {
    val orientation = LocalConfiguration.current.orientation
    DisposableEffect(window) {
        val oldContrast = if (Build.VERSION.SDK_INT >= 29) window.isNavigationBarContrastEnforced else false
        if (Build.VERSION.SDK_INT >= 29) window.isNavigationBarContrastEnforced = false
        onDispose {
            if (Build.VERSION.SDK_INT >= 29) window.isNavigationBarContrastEnforced = oldContrast
        }
    }
    DisposableEffect(window, view, visible, orientation) {
        val controller = WindowCompat.getInsetsController(window, view)
        val bars = WindowInsetsCompat.Type.systemBars()
        val cancellation = CancellationSignal()
        when {
            visible -> controller.show(bars)
            Build.VERSION.SDK_INT < 30 -> controller.hide(bars)
            else -> controller.controlWindowInsetsAnimation(
                bars, 0L, null, cancellation,
                object : WindowInsetsAnimationControlListenerCompat {
                    override fun onReady(animation: WindowInsetsAnimationControllerCompat, types: Int) {
                        animation.setInsetsAndAlpha(Insets.NONE, 1f, 0f)
                        animation.finish(false)
                    }

                    override fun onFinished(animation: WindowInsetsAnimationControllerCompat) = Unit

                    override fun onCancelled(animation: WindowInsetsAnimationControllerCompat?) {
                        if (!cancellation.isCanceled) controller.hide(bars)
                    }
                },
            )
        }
        onDispose { cancellation.cancel() }
    }
}
