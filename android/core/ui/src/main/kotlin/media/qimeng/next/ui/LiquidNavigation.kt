package media.qimeng.next.ui

import android.animation.ValueAnimator
import android.os.Build
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.spring
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlin.math.roundToInt

data class NavigationItem(val key: String, val label: String, val icon: ImageVector)

/** Backend-independent, reusable navigation. Content semantics belong to the caller. */
@Composable
fun LiquidNavigation(items: List<NavigationItem>, selected: Int, backdrop: GlassBackdrop,
                     reducedMotion: Boolean, glass: Boolean, onSelect: (Int) -> Unit,
                     modifier: Modifier = Modifier) {
    val capability = glassCapability(Build.VERSION.SDK_INT, LocalView.current.isHardwareAccelerated, glass)
    val noMotion = reducedMotion || !ValueAnimator.areAnimatorsEnabled()
    var drag by remember { mutableStateOf<Float?>(null) }
    var width by remember { mutableFloatStateOf(1f) }
    val animated by animateFloatAsState(drag ?: selected.toFloat(),
        if (noMotion || drag != null) snap() else spring(dampingRatio = 0.72f, stiffness = 420f), label = "selection")
    val haptic = LocalHapticFeedback.current
    val shape = RoundedCornerShape(100.dp)
    val height = NavigationDimensions.height()
    val slotPadding = NavigationDimensions.slotPadding
    val slotPaddingPx = with(LocalDensity.current) { slotPadding.toPx() }
    val colors = MaterialTheme.colorScheme
    val fill = if (capability == GlassCapability.SOLID) colors.surface else colors.surface.copy(alpha = 0.76f)
    val highlight = colors.primaryContainer.copy(alpha = if (capability == GlassCapability.SOLID) 1f else 0.62f)
    Box(modifier.onSizeChanged { width = it.width.toFloat().coerceAtLeast(1f) }
        .clip(shape)
        .then(rememberBackdropModifier(backdrop, { animated }, items.size, glass, slotPadding))
        .background(fill)
        .border(1.dp, colors.outlineVariant.copy(alpha = 0.55f), shape)
        .pointerInput(items.size, selected) {
            detectHorizontalDragGestures(
                onDragStart = { drag = ((it.x - slotPaddingPx) / (width - 2 * slotPaddingPx).coerceAtLeast(1f) * items.size - 0.5f).coerceIn(0f, (items.lastIndex).toFloat()) },
                onDragEnd = { val next = (drag ?: selected.toFloat()).roundToInt(); drag = null; if (next != selected) onSelect(next) },
                onDragCancel = { drag = null },
                onHorizontalDrag = { change, amount ->
                    change.consume()
                    drag = ((drag ?: selected.toFloat()) + amount / (width - 2 * slotPaddingPx).coerceAtLeast(1f) * items.size).coerceIn(0f, items.lastIndex.toFloat())
                },
            )
        }
        .drawBehind {
            val cell = (size.width - 2 * slotPadding.toPx()) / items.size
            val inset = 6.dp.toPx()
            drawRoundRect(highlight, Offset(slotPadding.toPx() + cell * animated + inset, inset), Size(cell - inset * 2, size.height - inset * 2), CornerRadius(size.height / 2))
        }
    ) {
        // A pointer-only sibling behind the controls consumes gaps without adding an empty
        // accessibility button or hiding the child tabs from TalkBack.
        Box(Modifier.matchParentSize().pointerInput(Unit) { detectTapGestures { } })
        Row(Modifier.fillMaxWidth().selectableGroup().padding(horizontal = slotPadding)) {
            items.forEachIndexed { index, item ->
                val iconScale by animateFloatAsState(if (index == selected) 1.12f else 1f,
                    if (noMotion) snap() else spring(dampingRatio = 0.5f, stiffness = 1500f), label = "navigation-icon")
                Column(Modifier.weight(1f).heightIn(min = height).clip(shape).testTag("navigation:${item.key}")
                    .selectable(selected = index == selected, role = Role.Tab, onClick = {
                        if (index != selected) {
                            if (!noMotion) haptic.performHapticFeedback(HapticFeedbackType.TextHandleMove)
                            onSelect(index)
                        }
                    }).padding(horizontal = 2.dp, vertical = 4.dp),
                    horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(2.dp, Alignment.CenterVertically)) {
                    val tint = if (index == selected) colors.primary else colors.onSurface
                    Icon(item.icon, contentDescription = null, tint = tint, modifier = Modifier.size(24.dp).graphicsLayer { scaleX = iconScale; scaleY = iconScale })
                    Text(item.label, color = tint, style = MaterialTheme.typography.labelMedium,
                        fontWeight = if (index == selected) FontWeight.SemiBold else FontWeight.Normal, maxLines = 1, textAlign = TextAlign.Center)
                }
            }
        }
    }
}
