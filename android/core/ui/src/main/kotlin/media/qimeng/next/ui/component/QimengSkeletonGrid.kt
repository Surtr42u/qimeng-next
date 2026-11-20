package media.qimeng.next.ui.component

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.aspectRatio
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
import androidx.compose.runtime.State
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
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
    // 不在这里读取动画值：脉动 alpha 以 State 下传，只在叶子的绘制阶段读取，
    // 整张骨架网与 Lazy 内容不再随每一帧抖动重组。
    val pulseAlpha = pulse.animateFloat(
        initialValue = SKELETON_MIN_ALPHA,
        targetValue = SKELETON_MAX_ALPHA,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = SKELETON_PULSE_HALF_CYCLE_MS, easing = LinearEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "qimengSkeletonPulseAlpha",
    )
    val density = LocalDensity.current
    val cellShape = remember(density) {
        RoundedCornerShape(with(density) { SKELETON_CARD_CORNER_RADIUS_PX.toDp() })
    }

    LazyVerticalGrid(
        columns = GridCells.Fixed(columns),
        modifier = modifier
            .padding(top = QimengDimens.SpaceM)
            .fillMaxSize(),
        horizontalArrangement = Arrangement.spacedBy(SKELETON_GRID_INTER_ITEM_SPACING),
        verticalArrangement = Arrangement.spacedBy(SKELETON_GRID_INTER_ITEM_SPACING),
        contentPadding = PaddingValues(start = QimengDimens.SpaceM, end = QimengDimens.SpaceM),
    ) {
        items(itemCount, contentType = { "skeleton" }) {
            SkeletonCell(shape = cellShape, pulseAlpha = pulseAlpha)
        }
    }
}

@Composable
private fun SkeletonCell(shape: RoundedCornerShape, pulseAlpha: State<Float>) {
    val color = MaterialTheme.colorScheme.surfaceVariant
    Box(
        modifier = Modifier
            .padding(SKELETON_CARD_OUTER_PADDING)
            .fillMaxWidth()
            .aspectRatio(16f / 9f)
            .clip(shape)
            // 只在绘制阶段读 State：动画每帧仅重绘，不触发重组。
            .drawBehind { drawRect(color = color, alpha = pulseAlpha.value) },
    )
}
