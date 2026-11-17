package media.qimeng.next.ui

import android.graphics.Bitmap
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Rule
import org.junit.Test
import java.io.File

class NavigationBehaviorTest {
    @get:Rule val compose = createComposeRule()

    @Test fun rapidSelectionKeepsExactlyOneLivePageAndRestoresDraft() {
        compose.setContent { Fixture() }
        compose.onNodeWithTag("draft").performTextInput("保留的草稿")
        compose.mainClock.autoAdvance = false
        repeat(5) { cycle ->
            compose.onNodeWithText("相册").performClick()
            compose.mainClock.advanceTimeBy(16)
            compose.onNodeWithText("我的").performClick()
            compose.mainClock.advanceTimeBy(16)
            compose.onNodeWithText("首页").performClick()
            compose.mainClock.advanceTimeBy(16)
            compose.onAllNodesWithTag("scene").assertCountEquals(1)
            if (cycle == 0) screenshot("navigation-rapid-first-frame.png")
        }
        compose.mainClock.autoAdvance = true
        compose.onNodeWithTag("draft").assertTextContains("保留的草稿")
    }

    @Test fun reducedMotionRetainsNavigationAndSelectedSemantics() {
        compose.setContent { Fixture(reduced = true) }
        compose.onNodeWithText("相册").performClick()
        compose.onNodeWithText("相册").assertIsSelected()
        compose.onAllNodesWithTag("scene").assertCountEquals(1)
        compose.onNodeWithTag("content:1").assertIsDisplayed()
    }

    @Test fun darkLargeTypeRetainsAllNavigationTargets() {
        compose.setContent {
            val density = LocalDensity.current
            CompositionLocalProvider(LocalDensity provides Density(density.density, fontScale = 2f)) { Fixture(dark = true) }
        }
        listOf("首页", "相册", "我的").forEach { compose.onNodeWithText(it).assertIsDisplayed().assertHasClickAction() }
        screenshot("navigation-dark-large-type.png")
    }

    @Test fun glassMaterialRendersAgainstSyntheticMovingContent() {
        compose.setContent { Fixture() }
        compose.onNodeWithText("相册").performClick()
        compose.waitForIdle()
        screenshot("navigation-light-glass.png")
    }

    private fun screenshot(name: String) {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val file = File(context.getExternalFilesDir(null), name)
        compose.waitForIdle()
        val screenshot = requireNotNull(InstrumentationRegistry.getInstrumentation().uiAutomation.takeScreenshot())
        file.outputStream().use { screenshot.compress(Bitmap.CompressFormat.PNG, 100, it) }
        screenshot.recycle()
    }
}

@Composable
private fun Fixture(reduced: Boolean = false, dark: Boolean = false) {
    QimengTheme(dark) {
        var selected by remember { mutableIntStateOf(0) }
        val backdrop = rememberGlassBackdrop()
        Box(Modifier.fillMaxSize()) {
            Box(Modifier.fillMaxSize().captureGlassBackdrop(backdrop)) {
                SingleSceneHost(selected.toString(), reduced) {
                    Column(Modifier.fillMaxSize().testTag("scene")) {
                        Text("页面 $selected", Modifier.testTag("content:$selected"))
                        var draft by rememberSaveable { mutableStateOf("") }
                        OutlinedTextField(draft, { draft = it }, Modifier.testTag("draft"))
                        Canvas(Modifier.fillMaxSize()) {
                            repeat(16) { i ->
                                drawRect(if ((i + selected) % 2 == 0) Color(0xFF546EB1) else Color(0xFFEEBA80),
                                    topLeft = Offset(i * size.width / 16, 0f), size = Size(size.width / 16, size.height))
                            }
                        }
                    }
                }
            }
            LiquidNavigation(listOf(NavigationItem("home", "首页", Icons.Outlined.Home), NavigationItem("albums", "相册", Icons.Outlined.PhotoLibrary),
                NavigationItem("me", "我的", Icons.Outlined.Person)), selected, backdrop, reduced, true, { selected = it },
                Modifier.align(Alignment.BottomCenter).padding(20.dp).fillMaxWidth())
        }
    }
}
