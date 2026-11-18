package media.qimeng.next.screen

import media.qimeng.next.model.AssetQuery
import media.qimeng.next.ui.component.*
import org.junit.Assert.*
import org.junit.Test

class BrowserControlsTest {
    @Test fun homePanelOnlyChangesCosRetrievalUntilReset() {
        val regular = BrowserControls()
        val cos = BrowserControls("cos")
        val recommendation = AssetQuery(source = "recommendations", seed = 18)
        val cosFeed = recommendation.copy(kind = "cos")
        val rankings = AssetQuery(source = "rankings", period = "day")
        assertEquals(cosFeed, cos.query(cosFeed))
        cos.panel = FilterPanelDraft(order = FilterOrder.ASC, tagMode = PanelTagMode.EXACT, tagIds = listOf("A", "B"),
            dateRange = PanelDateRange.YEAR_RANGE, yearFrom = 2025, yearTo = 2020)
        val filtered = cos.query(cosFeed)
        assertEquals("assets", filtered.source)
        assertEquals("cos", filtered.kind)
        assertEquals("asc", filtered.order)
        assertEquals(2020, filtered.fromYear)
        assertEquals(2025, filtered.toYear)
        assertEquals("all", filtered.tagMode)
        assertEquals(listOf("A", "B"), filtered.tags)
        assertEquals(recommendation, regular.query(recommendation))
        assertEquals(rankings, regular.query(rankings))
        cos.panel = FilterPanelDraft()
        assertEquals(cosFeed, cos.query(cosFeed))
    }
}
