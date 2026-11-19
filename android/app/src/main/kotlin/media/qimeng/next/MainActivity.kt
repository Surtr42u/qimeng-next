package media.qimeng.next

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.core.content.IntentCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.launch
import media.qimeng.next.screen.AppRoot

class MainActivity : ComponentActivity() {
    private val model by viewModels<LibraryViewModel>()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent { AppRoot(model) }
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                (application as QimengApplication).graph.thumbnails.prefetchWhileStarted()
            }
        }
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
}
