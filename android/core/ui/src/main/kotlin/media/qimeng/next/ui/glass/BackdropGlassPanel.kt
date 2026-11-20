package media.qimeng.next.ui.glass

import android.os.Build
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.kyant.backdrop.BackdropEffectScope
import com.kyant.backdrop.drawBackdrop
import com.kyant.backdrop.effects.blur
import com.kyant.backdrop.effects.vibrancy
import com.kyant.backdrop.shadow.Shadow
import media.qimeng.next.ui.theme.QimengDimens
import media.qimeng.next.ui.theme.QimengShapes
import media.qimeng.next.ui.theme.isQimengDarkTheme

private val CAPSULE_BLUR_RADIUS = 22.dp

private const val CAPSULE_SCRIM_ALPHA_LIGHT = 0.62f

private const val CAPSULE_SCRIM_ALPHA_DARK = 0.30f

private val CAPSULE_EDGE_WIDTH = 1.dp

@Composable
fun BackdropGlassPanel(
    backdrop: QimengBackdropState,
    modifier: Modifier = Modifier,
    shape: Shape = QimengShapes.pill,
    content: @Composable BoxScope.() -> Unit,
) {
    val glass = glassColors()
    val scrimAlpha = if (isQimengDarkTheme()) CAPSULE_SCRIM_ALPHA_DARK else CAPSULE_SCRIM_ALPHA_LIGHT
    val scrim = MaterialTheme.colorScheme.surface.copy(alpha = scrimAlpha)
    if (!backdropRenderEffectAvailable) {
        GlassSurface(shape = shape, modifier = modifier, content = content)
        return
    }
    val surface: DrawScope.() -> Unit = remember(scrim, glass) { { capsuleSurface(scrim, glass) } }
    // drawBackdrop 每次调用都会新建 ShapeProvider，元素按引用比较永不相等；只有把整条修饰链
    // 按参数记住、复用同一元素实例，头部玻璃重组（首页翻页/切列）时才不会让背板绘制节点反复失效。
    val drawModifier = remember(backdrop.backdrop, shape, surface) {
        Modifier.drawBackdrop(
            backdrop = backdrop.backdrop,
            shape = { shape },
            effects = CapsuleEffects,
            highlight = NoHighlight,
            shadow = NoShadow,
            onDrawSurface = surface,
        )
    }
    Box(
        modifier = modifier.then(drawModifier),
        content = content,
    )
}

@Composable
fun BackdropGlassIconButton(
    icon: ImageVector,
    contentDescription: String,
    onClick: () -> Unit,
    backdrop: QimengBackdropState,
    modifier: Modifier = Modifier,
    size: Dp = 40.dp,
    iconSize: Dp = QimengDimens.IconDefaultSize,
    tint: Color = MaterialTheme.colorScheme.onSurface,
) {
    val interaction = rememberPressScaleSource()
    Box(modifier = modifier.size(size)) {
        BackdropGlassPanel(
            backdrop = backdrop,
            modifier = Modifier
                .size(size)
                .pressScale(interaction, pressedScale = 0.94f)
                .clickable(interactionSource = interaction, indication = null, onClick = onClick),
        ) {
            Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                Icon(
                    imageVector = icon,
                    contentDescription = contentDescription,
                    tint = tint,
                    modifier = Modifier.size(iconSize),
                )
            }
        }
    }
}

private val CapsuleEffects: BackdropEffectScope.() -> Unit = {
    vibrancy()
    blur(CAPSULE_BLUR_RADIUS.toPx())
}

private val NoHighlight = { null }

private val NoShadow = { Shadow(alpha = 0f) }

private fun DrawScope.capsuleSurface(scrim: Color, glass: GlassColors) {
    drawRect(scrim)
    drawRoundRect(
        brush = Brush.verticalGradient(
            colors = listOf(glass.edgeTop, glass.edgeBottom),
            startY = 0f,
            endY = size.height,
        ),
        cornerRadius = CornerRadius(size.height / 2f),
        style = Stroke(width = CAPSULE_EDGE_WIDTH.toPx()),
    )
}

internal val backdropRenderEffectAvailable: Boolean = Build.VERSION.SDK_INT >= 31
