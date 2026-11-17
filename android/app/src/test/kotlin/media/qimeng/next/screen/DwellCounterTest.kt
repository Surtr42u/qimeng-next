package media.qimeng.next.screen

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class DwellCounterTest {
    @Test fun pausesAndRepeatedLifecycleEventsDoNotCountBackgroundTimeTwice() {
        var clock = 0L
        var starts = 0
        val segments = mutableListOf<Double>()
        val counter = DwellCounter({ starts++ }, { seconds, _ -> segments += seconds }, { clock }, { "2026-01-01T00:00:00Z" })
        counter.start(); counter.start()
        clock = 2_000; counter.stop(); counter.stop()
        clock = 60_000; counter.checkpoint()
        counter.start(); clock = 63_000; counter.stop()
        assertEquals(listOf(2.0, 3.0), segments)
        assertEquals(2, starts)
    }

    @Test fun checkpointsPersistElapsedSegmentsWithoutStartingAnotherView() {
        var clock = 0L
        var starts = 0
        val segments = mutableListOf<Double>()
        val counter = DwellCounter({ starts++ }, { seconds, _ -> segments += seconds }, { clock }, { "synthetic-start" })
        counter.start(); clock = 30_000; counter.checkpoint(); clock = 32_000; counter.stop()
        assertEquals(listOf(30.0, 2.0), segments)
        assertEquals(1, starts)
    }

    @Test fun lessThanOneSecondIsNotSent() {
        var clock = 0L
        val segments = mutableListOf<Double>()
        val counter = DwellCounter({}, { seconds, _ -> segments += seconds }, { clock }, { "synthetic-start" })
        counter.start(); clock = 999; counter.stop()
        assertTrue(segments.isEmpty())
    }
}
