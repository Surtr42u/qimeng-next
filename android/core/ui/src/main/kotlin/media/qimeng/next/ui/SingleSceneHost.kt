package media.qimeng.next.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds

/** Original routes switch in one frame. Keep scroll state and drafts across removal. */
@Composable
fun SingleSceneHost(key: String, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    val holder = rememberSaveableStateHolder()
    var recentDetailKeys by rememberSaveable { mutableStateOf(emptyList<String>()) }
    SideEffect {
        if (':' in key && !key.startsWith("tab:") && recentDetailKeys.lastOrNull() != key) {
            val keys = recentDetailKeys.filterNot { it == key } + key
            keys.dropLast(12).forEach(holder::removeState)
            recentDetailKeys = keys.takeLast(12)
        }
    }
    Box(modifier.fillMaxSize().clipToBounds().background(MaterialTheme.colorScheme.background)) {
        holder.SaveableStateProvider(key, content)
    }
}
