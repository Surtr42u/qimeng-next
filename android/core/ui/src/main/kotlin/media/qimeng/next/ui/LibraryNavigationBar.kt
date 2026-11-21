package media.qimeng.next.ui

import androidx.compose.material3.*
import androidx.compose.animation.core.Animatable
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.inset
import androidx.compose.ui.platform.testTag
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import media.qimeng.next.model.TabBarMaterial
import media.qimeng.next.ui.glass.FloatingTabDock
import media.qimeng.next.ui.glass.GlassNavItem
import media.qimeng.next.ui.glass.QimengBackdropState
import media.qimeng.next.ui.glass.TabDockDefaults
import media.qimeng.next.ui.theme.QimengBrandColors
import media.qimeng.next.ui.theme.isQimengDarkTheme

/** ClassicBottomBar and FloatingTabDock are the two original render paths. */
@Composable
fun LibraryNavigationBar(items: List<GlassNavItem>, selected: Int, material: TabBarMaterial, backdrop: QimengBackdropState?,
    onSelect: (Int) -> Unit, modifier: Modifier = Modifier, visible: Boolean = true) {
    // 选中丸的动画位置记在导航栏这一层：外壳在 pushed 路由上仍保留本组合（visible=false
    // 时不出树），返回首页时丸从上次位置直接出现，不会因为重组出 alpha=0 的新 Animatable 弹入。
    val pillCenter = remember { Animatable(Float.NaN) }
    if (!visible) return
    if (material != TabBarMaterial.CLASSIC) {
        FloatingTabDock(items, selected, onSelect, material, backdrop, modifier, pillCenter)
        return
    }
    val indicator = if (isQimengDarkTheme()) QimengBrandColors.PrimarySoftDark else QimengBrandColors.PrimarySoftLight
    val indicatorHeight = TabDockDefaults.indicatorHeight()
    NavigationBar(modifier) {
        items.forEachIndexed { index, item ->
            NavigationBarItem(selected == index, { onSelect(index) },
                icon = { Icon(item.icon, null) }, label = { Text(item.label) },
                modifier = Modifier.testTag("navigation:${item.key}").drawBehind {
                    if (selected == index) {
                        val verticalInset = ((size.height - indicatorHeight.toPx()) / 2f).coerceAtLeast(0f)
                        inset(horizontal = TabDockDefaults.IndicatorInset.toPx(), vertical = verticalInset) {
                            drawRoundRect(indicator, cornerRadius = CornerRadius(size.minDimension / 2f))
                        }
                    }
                },
                colors = NavigationBarItemDefaults.colors(indicatorColor = Color.Transparent, selectedIconColor = MaterialTheme.colorScheme.onSurface,
                    selectedTextColor = MaterialTheme.colorScheme.onSurface))
        }
    }
}

object TabScrollController {
    private val requests = MutableSharedFlow<Int>(extraBufferCapacity = 1)
    val events = requests.asSharedFlow()
    fun scrollToTop(tab: Int) { requests.tryEmit(tab) }
}
