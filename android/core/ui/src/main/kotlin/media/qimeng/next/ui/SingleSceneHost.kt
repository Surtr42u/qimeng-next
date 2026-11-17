package media.qimeng.next.ui

import android.animation.ValueAnimator
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.spring
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.dp

/** One interactive scene at a time. Interrupted transitions cannot retain a stale page. */
@Composable
fun SingleSceneHost(key: String, reducedMotion: Boolean, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    val holder = rememberSaveableStateHolder()
    val distance = with(LocalDensity.current) { 24.dp.toPx() }
    val disabled = reducedMotion || !ValueAnimator.areAnimatorsEnabled()
    val entrance = remember(key, disabled) { Animatable(if (disabled) 0f else distance) }
    LaunchedEffect(key, disabled) {
        if (disabled) entrance.snapTo(0f)
        else entrance.animateTo(0f, spring(dampingRatio = 0.92f, stiffness = 450f))
    }
    Box(modifier.fillMaxSize().clipToBounds().background(MaterialTheme.colorScheme.background)) {
        // Initial translation is constructed with the page, before its first draw.
        // Each scene's scroll position and drafts survive removal from composition.
        holder.SaveableStateProvider(key) {
            Box(Modifier.fillMaxSize().graphicsLayer { translationX = entrance.value }) { content() }
        }
    }
}
