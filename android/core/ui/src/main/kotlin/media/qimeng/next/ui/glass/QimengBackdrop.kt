package media.qimeng.next.ui.glass

import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import com.kyant.backdrop.backdrops.LayerBackdrop
import com.kyant.backdrop.backdrops.layerBackdrop
import com.kyant.backdrop.backdrops.rememberLayerBackdrop

class QimengBackdropState internal constructor(
    internal val backdrop: LayerBackdrop,
)

@Composable
fun rememberQimengBackdropState(baseColor: Color): QimengBackdropState {
    val backdrop = rememberLayerBackdrop {
        drawRect(baseColor)
        drawContent()
    }
    return remember(backdrop) { QimengBackdropState(backdrop) }
}

fun Modifier.qimengBackdropSource(state: QimengBackdropState): Modifier =
    this.layerBackdrop(state.backdrop)
