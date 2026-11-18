package media.qimeng.next.ui.component

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.dp
import media.qimeng.next.ui.theme.QimengDimens

const val QIMENG_SKELETON_GRID_ROWS = 6


private val SKELETON_GRID_INTER_ITEM_SPACING = 0.dp

private val SKELETON_CARD_OUTER_PADDING = 5.dp

private const val SKELETON_CARD_CORNER_RADIUS_PX = 24f


private const val SKELETON_PULSE_HALF_CYCLE_MS = 400

private const val SKELETON_MIN_ALPHA = 0.45f

private const val SKELETON_MAX_ALPHA = 1.0f

@Composable
fun QimengSkeletonGrid(
    columns: Int,
    itemCount: Int,
    modifier: Modifier = Modifier,
) {
    val pulse = rememberInfiniteTransition(label = "qimengSkeletonPulse")
    val pulseAlpha by pulse.animateFloat(
        initialValue = SKELETON_MIN_ALPHA,
        targetValue = SKELETON_MAX_ALPHA,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = SKELETON_PULSE_HALF_CYCLE_MS, easing = LinearEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "qimengSkeletonPulseAlpha",
    )
    val cellShape = RoundedCornerShape(
        with(LocalDensity.current) { SKELETON_CARD_CORNER_RADIUS_PX.toDp() },
    )

    LazyVerticalGrid(
        columns = GridCells.Fixed(columns),
        modifier = modifier
            .padding(top = QimengDimens.SpaceM)
            .fillMaxSize(),
        horizontalArrangement = Arrangement.spacedBy(SKELETON_GRID_INTER_ITEM_SPACING),
        verticalArrangement = Arrangement.spacedBy(SKELETON_GRID_INTER_ITEM_SPACING),
        contentPadding = PaddingValues(start = QimengDimens.SpaceM, end = QimengDimens.SpaceM),
    ) {
        items(itemCount) {
            SkeletonCell(shape = cellShape, pulseAlpha = pulseAlpha)
        }
    }
}

@Composable
private fun SkeletonCell(shape: RoundedCornerShape, pulseAlpha: Float) {
    Box(
        modifier = Modifier
            .padding(SKELETON_CARD_OUTER_PADDING)
            .fillMaxWidth()
            .aspectRatio(16f / 9f)
            .clip(shape)
            .background(MaterialTheme.colorScheme.surfaceVariant.copy(alpha = pulseAlpha)),
    )
}
