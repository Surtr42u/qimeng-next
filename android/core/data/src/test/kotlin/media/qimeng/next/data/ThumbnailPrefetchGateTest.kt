package media.qimeng.next.data

import kotlin.random.Random
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ThumbnailPrefetchGateTest {
    private val otherPolicy = ThumbnailCachePolicy.VERSION + "-next"
    private val record = ThumbnailPrefetchRecord("nas:fixture", 7, ThumbnailCachePolicy.VERSION)

    @Test fun skipRequiresMatchingServerRevisionAndThumbnailPolicy() {
        assertEquals(
            ThumbnailPrefetchDecision.SKIP,
            ThumbnailPrefetchGate.decide(7, record, "nas:fixture", ThumbnailCachePolicy.VERSION),
        )
        assertEquals(
            ThumbnailPrefetchDecision.FULL,
            ThumbnailPrefetchGate.decide(null, record, "nas:fixture", ThumbnailCachePolicy.VERSION),
        )
        assertEquals(
            ThumbnailPrefetchDecision.FULL,
            ThumbnailPrefetchGate.decide(7, null, "nas:fixture", ThumbnailCachePolicy.VERSION),
        )
        assertEquals(
            "A different server with a colliding counter must not skip",
            ThumbnailPrefetchDecision.FULL,
            ThumbnailPrefetchGate.decide(7, record, "nas:other", ThumbnailCachePolicy.VERSION),
        )
        assertEquals(
            ThumbnailPrefetchDecision.FULL,
            ThumbnailPrefetchGate.decide(8, record, "nas:fixture", ThumbnailCachePolicy.VERSION),
        )
        assertEquals(
            "A preview policy change must invalidate the cache",
            ThumbnailPrefetchDecision.FULL,
            ThumbnailPrefetchGate.decide(7, record, "nas:fixture", otherPolicy),
        )
    }

    @Test fun sampleDowngradesAtTwentyPercentMissing() {
        assertFalse(ThumbnailPrefetchSampleGate.shouldDowngradeToFull(0, 0))
        assertFalse(ThumbnailPrefetchSampleGate.shouldDowngradeToFull(64, 12))
        assertTrue(ThumbnailPrefetchSampleGate.shouldDowngradeToFull(64, 13))
        assertFalse(ThumbnailPrefetchSampleGate.shouldDowngradeToFull(10, 1))
        assertTrue(ThumbnailPrefetchSampleGate.shouldDowngradeToFull(10, 2))
    }

    @Test fun sampleKeepsSmallLibrariesAndCapsLargeOnes() {
        val small = (1L..10L).map { ThumbnailPrefetchSample("sample:$it", it) }
        assertEquals(small.toSet(), ThumbnailPrefetchSampleGate.pickSample(small, Random(1)))
        val large = (1L..200L).map { ThumbnailPrefetchSample("sample:$it", it) }
        val picked = ThumbnailPrefetchSampleGate.pickSample(large, Random(7))
        assertEquals(ThumbnailPrefetchSampleGate.SAMPLE_SIZE, picked.size)
        assertTrue(large.toSet().containsAll(picked))
    }

    @Test fun sampleEncodingRoundTripsAndRejectsGarbage() {
        val sample = ThumbnailPrefetchSample("aa:bb", 12)
        assertEquals(sample, ThumbnailPrefetchSample.decode(sample.encode()))
        assertNull(ThumbnailPrefetchSample.decode(""))
        assertNull(ThumbnailPrefetchSample.decode("12"))
        assertNull(ThumbnailPrefetchSample.decode("0|aa"))
        assertNull(ThumbnailPrefetchSample.decode("12|"))
    }

    @Test fun turnoverTakesOverAForeignPoolAndCapsWritesOnAnOwnedPool() {
        val pool = 1024L * 1024 * 1024
        // 空池：没有可淘汰的条目，按刷新额度收口（额度只在池满时才消耗）。
        assertEquals(ThumbnailTurnoverPolicy.REFRESH_ENTRIES, ThumbnailTurnoverPolicy.limit(0, 0))
        // 池里全是他人的条目：接管（否则新实例一张也预取不进）。
        assertEquals(Long.MAX_VALUE, ThumbnailTurnoverPolicy.limit(0, pool))
        // 恰好一半是别人的条目仍按接管处理（>= 阈值）。
        assertEquals(Long.MAX_VALUE, ThumbnailTurnoverPolicy.limit(pool / 2, pool))
        // 自己已占多数：只给新增媒体的刷新额度。
        assertEquals(ThumbnailTurnoverPolicy.REFRESH_ENTRIES, ThumbnailTurnoverPolicy.limit(pool / 2 + 1, pool))
        assertEquals(ThumbnailTurnoverPolicy.REFRESH_ENTRIES, ThumbnailTurnoverPolicy.limit(pool, pool))
        // 统计偏大也不能得出「负数他人条目 → 接管」。
        assertEquals(ThumbnailTurnoverPolicy.REFRESH_ENTRIES, ThumbnailTurnoverPolicy.limit(pool * 2, pool))
    }

    @Test fun turnoverBudgetStopsAtItsLimitAndUnlimitedNeverExhausts() {
        val budget = ThumbnailTurnoverBudget(2L)
        assertTrue(budget.tryAcquire())
        assertTrue(budget.tryAcquire())
        assertFalse(budget.tryAcquire())
        assertFalse(ThumbnailTurnoverBudget(0L).tryAcquire())
        val unlimited = ThumbnailTurnoverBudget(Long.MAX_VALUE)
        repeat(64) { assertTrue(unlimited.tryAcquire()) }
    }
}
