package media.qimeng.next.ui

import media.qimeng.next.model.Preferences
import media.qimeng.next.model.TabBarMaterial
import org.junit.Assert.assertEquals
import org.junit.Test

class NavigationMaterialsTest {
    @Test fun firstLaunchUsesTheOriginalDefaultAndOnlyGlassModesCaptureContent() {
        assertEquals(TabBarMaterial.LIQUID, Preferences().material)
        assertEquals(listOf(TabBarMaterial.LIQUID, TabBarMaterial.FROSTED), TabBarMaterial.entries.filter { it.usesBackdrop })
    }
}
