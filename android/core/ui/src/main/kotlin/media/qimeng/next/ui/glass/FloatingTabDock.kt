package media.qimeng.next.ui.glass

import android.os.Build
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.spring
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.kyant.backdrop.Backdrop
import com.kyant.backdrop.BackdropEffectScope
import com.kyant.backdrop.drawBackdrop
import com.kyant.backdrop.effects.blur
import com.kyant.backdrop.effects.lens
import com.kyant.backdrop.effects.vibrancy
import com.kyant.backdrop.backdrops.layerBackdrop
import com.kyant.backdrop.backdrops.rememberCombinedBackdrop
import com.kyant.backdrop.backdrops.rememberLayerBackdrop
import com.kyant.backdrop.shadow.Shadow
import media.qimeng.next.model.TabBarMaterial
import media.qimeng.next.ui.LocalReducedMotion
import media.qimeng.next.ui.component.consumeTaps
import media.qimeng.next.ui.theme.QimengShapes
import media.qimeng.next.ui.theme.isQimengDarkTheme

@Composable
fun FloatingTabDock(
    items: List<GlassNavItem>,
    selectedIndex: Int,
    onSelect: (Int) -> Unit,
    material: TabBarMaterial,
    backdrop: QimengBackdropState?,
    modifier: Modifier = Modifier,
) {
    val capability = resolveBackdropCapabilities()
    val dockEffects = when (material) {
        TabBarMaterial.LIQUID -> liquidEffects(capability == DockGlassCapability.Full)
        else -> frostedEffects()
    }
    val scrimAlpha = if (material == TabBarMaterial.FROSTED) FROSTED_SCRIM_ALPHA else LIQUID_SCRIM_ALPHA
    val lensPill = (material == TabBarMaterial.LIQUID || material == TabBarMaterial.FROSTED) &&
        capability == DockGlassCapability.Full &&
        backdrop != null

    Column(
        modifier = modifier
            .fillMaxWidth()
            .consumeTaps(),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .consumeTaps(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Spacer(
                modifier = Modifier
                    .width(TabDockDefaults.DockHorizontalMargin)
                    .height(TabDockDefaults.DockHeight)
                    .consumeTaps(),
            )
            DockBody(
                material = material,
                backdrop = backdrop?.backdrop,
                dockEffects = dockEffects,
                scrimAlpha = scrimAlpha,
                modifier = Modifier
                    .weight(1f)
                    .consumeTaps(),
            ) {
                DockItemsRow(
                    items = items,
                    selectedIndex = selectedIndex.coerceIn(0, (items.size - 1).coerceAtLeast(0)),
                    onSelect = onSelect,
                    lensPill = lensPill,
                    externalBackdrop = backdrop?.backdrop,
                    dockEffects = dockEffects,
                    scrimAlpha = scrimAlpha,
                )
            }
            Spacer(
                modifier = Modifier
                    .width(TabDockDefaults.DockHorizontalMargin)
                    .height(TabDockDefaults.DockHeight)
                    .consumeTaps(),
            )
        }
        Spacer(
            modifier = Modifier
                .fillMaxWidth()
                .height(TabDockDefaults.DockBottomOffset)
                .navigationBarsPadding()
                .consumeTaps(),
        )
    }
}

object TabDockDefaults {

    val DockHeight: Dp = 62.dp

    val DockBottomOffset: Dp = 10.dp

    val DockHorizontalMargin: Dp = 16.dp

    val SlotInnerPadding: Dp = 18.dp

    val DockClearance: Dp = DockHeight + DockBottomOffset

    val IndicatorInset: Dp = 4.dp

    @Composable
    fun indicatorHeight(): Dp {
        val labelHeight = with(LocalDensity.current) { MaterialTheme.typography.labelMedium.lineHeight.toDp() }
        return maxOf(DockHeight - IndicatorInset * 2, IconSize + LABEL_TOP_SPACING + labelHeight + IndicatorInset * 2)
    }

    val IconSize: Dp = 24.dp

    @Composable
    fun bottomClearance(): Dp =
        WindowInsets.navigationBars
            .asPaddingValues()
            .calculateBottomPadding() +
            maxOf(DockClearance, CLASSIC_NAV_BAR_HEIGHT)

    private val CLASSIC_NAV_BAR_HEIGHT: Dp = 80.dp
}

data class GlassNavItem(
    val label: String,
    val icon: ImageVector,
    val key: String = label,
)

private const val PILL_TINT_ALPHA = 0.30f

private const val ICON_SELECTED_SCALE = 1.12f

private val LENS_PILL_SURFACE_DARK = Color(0xFFFFFFFF).copy(alpha = 0.10f)

private val LENS_PILL_SURFACE_LIGHT = Color(0xFF000000).copy(alpha = 0.10f)

@Composable
private fun DockBody(
    material: TabBarMaterial,
    backdrop: Backdrop?,
    dockEffects: BackdropEffectScope.() -> Unit,
    scrimAlpha: Float,
    modifier: Modifier = Modifier,
    content: @Composable BoxScope.() -> Unit,
) {
    val capability = resolveBackdropCapabilities()
    when (material) {
        TabBarMaterial.LIQUID, TabBarMaterial.FROSTED ->
            if (capability == DockGlassCapability.Pseudo || backdrop == null) {
                PseudoGlassDockBody(modifier, content)
            } else {
                GlassDockBody(
                    backdrop = backdrop,
                    scrimAlpha = scrimAlpha,
                    effects = dockEffects,
                    modifier = modifier,
                    content = content,
                )
            }

        TabBarMaterial.SOLID -> SolidDockBody(modifier, content)

        TabBarMaterial.CLASSIC -> PseudoGlassDockBody(modifier, content)
    }
}

private fun liquidEffects(full: Boolean): BackdropEffectScope.() -> Unit = {
    vibrancy()
    blur(LIQUID_BLUR_RADIUS.toPx())
    if (full) {
        lens(
            refractionHeight = LENS_REFRACTION_HEIGHT.toPx(),
            refractionAmount = LENS_REFRACTION_AMOUNT.toPx(),
        )
    }
}

private fun frostedEffects(): BackdropEffectScope.() -> Unit = {
    blur(FROSTED_BLUR_RADIUS.toPx())
}

@Composable
private fun GlassDockBody(
    backdrop: Backdrop,
    scrimAlpha: Float,
    effects: BackdropEffectScope.() -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable BoxScope.() -> Unit,
) {
    val glass = glassColors()
    val scrimColor = MaterialTheme.colorScheme.surface.copy(alpha = scrimAlpha)
    Box(
        modifier = modifier
            .height(TabDockDefaults.DockHeight)
            .drawBackdrop(
                backdrop = backdrop,
                shape = { QimengShapes.pill },
                effects = effects,
                highlight = { null },
                onDrawSurface = {
                    drawRect(scrimColor)
                    drawRoundRect(
                        brush = Brush.verticalGradient(
                            colors = listOf(glass.edgeTop, glass.edgeBottom),
                            startY = 0f,
                            endY = size.height,
                        ),
                        cornerRadius = CornerRadius(size.height / 2f),
                        style = Stroke(width = DOCK_EDGE_STROKE_WIDTH.toPx()),
                    )
                },
            ),
        content = content,
    )
}

@Composable
private fun PseudoGlassDockBody(
    modifier: Modifier = Modifier,
    content: @Composable BoxScope.() -> Unit,
) {
    GlassSurface(
        shape = QimengShapes.pill,
        strong = true,
        elevation = PSEUDO_GLASS_ELEVATION,
        modifier = modifier.height(TabDockDefaults.DockHeight),
        content = content,
    )
}

@Composable
private fun SolidDockBody(
    modifier: Modifier = Modifier,
    content: @Composable BoxScope.() -> Unit,
) {
    val glass = glassColors()
    Box(
        modifier = modifier
            .height(TabDockDefaults.DockHeight)
            .shadow(
                elevation = SOLID_DOCK_ELEVATION,
                shape = QimengShapes.pill,
                ambientColor = glass.shadow,
                spotColor = glass.shadow,
            )
            .clip(QimengShapes.pill)
            .background(MaterialTheme.colorScheme.surfaceContainerHighest),
        content = content,
    )
}

@Composable
private fun DockItemsRow(
    items: List<GlassNavItem>,
    selectedIndex: Int,
    onSelect: (Int) -> Unit,
    lensPill: Boolean,
    externalBackdrop: Backdrop?,
    dockEffects: BackdropEffectScope.() -> Unit,
    scrimAlpha: Float,
) {
    val density = LocalDensity.current
    var rowWidthPx by remember { mutableIntStateOf(0) }
    val slotInnerPaddingPx = with(density) { TabDockDefaults.SlotInnerPadding.toPx() }
    val slotWidthPx =
        if (rowWidthPx > 0 && items.isNotEmpty()) {
            (rowWidthPx - 2 * slotInnerPaddingPx) / items.size
        } else {
            0f
        }
    val indicatorInsetPx = with(density) { TabDockDefaults.IndicatorInset.toPx() }
    val pillWidthPx = (slotWidthPx - 2 * indicatorInsetPx).coerceAtLeast(0f)
    val pillCenter = remember { Animatable(Float.NaN) }
    val reduced = LocalReducedMotion.current
    LaunchedEffect(slotWidthPx, selectedIndex, reduced) {
        if (slotWidthPx <= 0f) return@LaunchedEffect
        val target = slotInnerPaddingPx + selectedIndex * slotWidthPx + slotWidthPx / 2f
        if (pillCenter.value.isNaN() || reduced) {
            pillCenter.snapTo(target)
        } else {
            pillCenter.animateTo(
                target,
                spring(
                    dampingRatio = Spring.DampingRatioMediumBouncy,
                    stiffness = Spring.StiffnessMediumLow,
                ),
            )
        }
    }
    val pillTint = MaterialTheme.colorScheme.primary.copy(alpha = PILL_TINT_ALPHA)
    val useLensPill = lensPill && externalBackdrop != null
    val tabsBackdrop = rememberLayerBackdrop()
    val lensBackdrop = if (useLensPill && pillWidthPx > 0 && externalBackdrop != null) {
        rememberCombinedBackdrop(externalBackdrop, tabsBackdrop)
    } else {
        null
    }
    val accentColor = MaterialTheme.colorScheme.primary
    val replicaScrim = MaterialTheme.colorScheme.surface.copy(alpha = scrimAlpha)
    val lensPillSurface = if (isQimengDarkTheme()) LENS_PILL_SURFACE_DARK else LENS_PILL_SURFACE_LIGHT

    Box(modifier = Modifier.consumeTaps()) {
        Row(
            modifier = Modifier
                .height(TabDockDefaults.DockHeight)
                .onSizeChanged { rowWidthPx = it.width }
                .padding(horizontal = TabDockDefaults.SlotInnerPadding),
        ) {
            items.forEachIndexed { index, item ->
                DockNavItem(
                    item = item,
                    selected = index == selectedIndex,
                    onClick = { onSelect(index) },
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxHeight(),
                )
            }
        }

        if (useLensPill && externalBackdrop != null) {
            Row(
                modifier = Modifier
                    .clearAndSetSemantics {}
                    .alpha(0f)
                    .layerBackdrop(tabsBackdrop)
                    .drawBackdrop(
                        backdrop = externalBackdrop,
                        shape = { QimengShapes.pill },
                        effects = dockEffects,
                        highlight = { null },
                        onDrawSurface = {
                            drawRect(replicaScrim)
                        },
                    )
                    .height(TabDockDefaults.DockHeight)
                    .fillMaxWidth()
                    .padding(horizontal = TabDockDefaults.SlotInnerPadding)
                    .graphicsLayer(colorFilter = ColorFilter.tint(accentColor)),
            ) {
                items.forEachIndexed { index, item ->
                    DockNavReplicaItem(
                        item = item,
                        selected = index == selectedIndex,
                        modifier = Modifier
                            .weight(1f)
                            .fillMaxHeight(),
                    )
                }
            }
        }

        if (pillWidthPx > 0) {
            Box(
                modifier = Modifier
                    .align(Alignment.CenterStart)
                    .width(with(density) { pillWidthPx.toDp() })
                    .height(TabDockDefaults.indicatorHeight().coerceAtMost(TabDockDefaults.DockHeight))
                    .graphicsLayer {
                        val center = pillCenter.value
                        if (center.isNaN()) {
                            alpha = 0f
                        } else {
                            alpha = 1f
                            translationX = center - pillWidthPx / 2f
                        }
                    }
                    .then(
                        if (useLensPill && lensBackdrop != null) {
                            Modifier.drawBackdrop(
                                backdrop = lensBackdrop,
                                shape = { QimengShapes.pill },
                                effects = { },
                                highlight = { null },
                                shadow = { Shadow(alpha = 0f) },
                                onDrawSurface = {
                                    drawRect(lensPillSurface)
                                },
                            )
                        } else {
                            Modifier.background(
                                color = pillTint,
                                shape = RoundedCornerShape(PILL_CORNER_PERCENT),
                            )
                        },
                    ),
            )
        }
    }
}

@Composable
private fun DockNavReplicaItem(
    item: GlassNavItem,
    selected: Boolean,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier,
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Box(contentAlignment = Alignment.Center) {
            Icon(
                imageVector = item.icon,
                contentDescription = null,
                modifier = Modifier
                    .size(TabDockDefaults.IconSize)
                    .graphicsLayer {
                        scaleX = if (selected) ICON_SELECTED_SCALE else 1f
                        scaleY = if (selected) ICON_SELECTED_SCALE else 1f
                    },
            )
        }
        Spacer(Modifier.height(LABEL_TOP_SPACING))
        Text(
            text = item.label,
            style = MaterialTheme.typography.labelMedium,
            fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
            maxLines = 1,
        )
    }
}

@Composable
private fun DockNavItem(
    item: GlassNavItem,
    selected: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val interactionSource = remember { MutableInteractionSource() }
    val iconScale by animateFloatAsState(
        targetValue = if (selected) ICON_SELECTED_SCALE else 1f,
        animationSpec = if (LocalReducedMotion.current) androidx.compose.animation.core.snap() else spring(
            dampingRatio = Spring.DampingRatioMediumBouncy,
            stiffness = Spring.StiffnessMedium,
        ),
        label = "dockIconScale",
    )
    val tint = if (selected) {
        MaterialTheme.colorScheme.primary
    } else {
        MaterialTheme.colorScheme.onSurface
    }
    Column(
        modifier = modifier
            .testTag("navigation:${item.key}")
            .clickable(
                interactionSource = interactionSource,
                indication = null,
                onClick = onClick,
            )
            .semantics {
                role = Role.Tab
                this.selected = selected
            },
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Box(contentAlignment = Alignment.Center) {
            Icon(
                imageVector = item.icon,
                contentDescription = null,
                tint = tint,
                modifier = Modifier
                    .size(TabDockDefaults.IconSize)
                    .graphicsLayer {
                        scaleX = iconScale
                        scaleY = iconScale
                    },
            )
        }
        Spacer(Modifier.height(LABEL_TOP_SPACING))
        Text(
            text = item.label,
            style = MaterialTheme.typography.labelMedium,
            fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
            color = tint,
            maxLines = 1,
        )
    }
}


private val LABEL_TOP_SPACING = 2.dp

private const val PILL_CORNER_PERCENT = 100

private val LIQUID_BLUR_RADIUS = 10.dp

private val FROSTED_BLUR_RADIUS = 22.dp

private val LENS_REFRACTION_HEIGHT = 16.dp

private val LENS_REFRACTION_AMOUNT = 32.dp

private const val LIQUID_SCRIM_ALPHA = 0.40f

private const val FROSTED_SCRIM_ALPHA = 0.62f

private val PSEUDO_GLASS_ELEVATION = 14.dp

private val SOLID_DOCK_ELEVATION = 6.dp

private val DOCK_EDGE_STROKE_WIDTH = 1.dp

private enum class DockGlassCapability {
    Full,
    BlurOnly,
    Pseudo,
}

private fun resolveBackdropCapabilities(): DockGlassCapability = when {
    Build.VERSION.SDK_INT >= 33 -> DockGlassCapability.Full
    Build.VERSION.SDK_INT >= 31 -> DockGlassCapability.BlurOnly
    else -> DockGlassCapability.Pseudo
}
