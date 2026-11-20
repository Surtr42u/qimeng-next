package media.qimeng.next

import android.animation.ValueAnimator
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.core.content.IntentCompat
import media.qimeng.next.screen.AppRoot

class MainActivity : ComponentActivity() {
    private val model by viewModels<LibraryViewModel>()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        followDisplayRefreshRate()
        setContent { AppRoot(model) }
        if (savedInstanceState == null) receiveShare(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        // Consume the new transient grants without replacing the entry intent retained across recreation.
        receiveShare(intent)
    }

    override fun onStart() {
        super.onStart()
        model.restoreLocalConnection()
    }

    private fun receiveShare(intent: Intent) {
        val uris = when (intent.action) {
            Intent.ACTION_SEND -> listOfNotNull(IntentCompat.getParcelableExtra(intent, Intent.EXTRA_STREAM, Uri::class.java))
            Intent.ACTION_SEND_MULTIPLE -> IntentCompat.getParcelableArrayListExtra(intent, Intent.EXTRA_STREAM, Uri::class.java).orEmpty()
            else -> return
        }
        val selection = uris.ifEmpty { intent.clipData?.let { clips -> (0 until clips.itemCount).mapNotNull { clips.getItemAt(it).uri } }.orEmpty() }
        if (selection.isNotEmpty()) model.receiveFiles(selection, shared = true)
    }

    /**
     * Compose only renders as fast as the window is allowed to. Without a window hint, OEM frame-rate
     * policies commonly pin apps that never request a rate to 60Hz, so ask for the panel's highest mode.
     * Battery saver and reduced motion (the in-app preference is loaded synchronously, see SecureSettings)
     * keep the platform default, and a mode that is already at least that fast is never lowered.
     */
    private fun followDisplayRefreshRate() {
        try {
            val power = getSystemService(PowerManager::class.java)
            if (power == null || power.isPowerSaveMode) return
            if (model.preferences.value.reducedMotion) return
            if (!ValueAnimator.areAnimatorsEnabled()) return
            @Suppress("DEPRECATION")
            val active = (if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) display else windowManager.defaultDisplay)
                ?: return
            val highest = active.supportedModes.maxOfOrNull { it.refreshRate } ?: return
            if (highest <= active.mode.refreshRate + REFRESH_RATE_MARGIN) return
            window.attributes = window.attributes.apply { preferredRefreshRate = highest }
        } catch (_: RuntimeException) {
            // A display or window that rejects the hint keeps the platform default rate.
        }
    }

    private companion object {
        const val REFRESH_RATE_MARGIN = 0.5f
    }
}
