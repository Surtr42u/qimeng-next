package media.qimeng.next.ui

import androidx.compose.material3.*
import androidx.compose.runtime.Composable
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
    onSelect: (Int) -> Unit, modifier: Modifier = Modifier) {
    if (material != TabBarMaterial.CLASSIC) {
        FloatingTabDock(items, selected, onSelect, material, backdrop, modifier)
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
