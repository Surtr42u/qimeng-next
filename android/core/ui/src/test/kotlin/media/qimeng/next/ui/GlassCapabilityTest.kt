package media.qimeng.next.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class GlassCapabilityTest {
    @Test fun refractionIsOnlyAdvertisedWithItsRequiredRenderer() {
        assertEquals(GlassCapability.REFRACTED, glassCapability(33, true, true))
        assertEquals(GlassCapability.BLURRED, glassCapability(31, true, true))
        assertEquals(GlassCapability.SOLID, glassCapability(30, true, true))
        assertEquals(GlassCapability.SOLID, glassCapability(35, false, true))
        assertEquals(GlassCapability.SOLID, glassCapability(35, true, false))
    }
}
