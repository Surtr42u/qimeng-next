package media.qimeng.next

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * 翻页失败重试节奏锁定（旧版问题A「翻页失败即卡死」修复口径）：
 * 只有翻页退避重试，首载/显式刷新/筛选变化立即失败；延迟与旧版
 * ListQueryDefaults.APPEND_RETRY_DELAYS_MS 一致（1s、3s），两次封顶。
 */
class AppendRetryScheduleTest {

    @Test fun firstPageRefreshAndFilterChangesFailImmediately() {
        assertNull(appendRetryDelayMs(append = false, attempt = 0))
        assertNull(appendRetryDelayMs(append = false, attempt = 1))
        assertNull(appendRetryDelayMs(append = false, attempt = 7))
    }

    @Test fun appendWaitsTheOldOneAndThreeSecondDelays() {
        assertEquals(listOf(1_000L, 3_000L), APPEND_RETRY_DELAYS_MS)
        assertEquals(1_000L, appendRetryDelayMs(append = true, attempt = 0))
        assertEquals(3_000L, appendRetryDelayMs(append = true, attempt = 1))
    }

    @Test fun appendStopsAfterTheScheduleIsExhausted() {
        assertNull(appendRetryDelayMs(append = true, attempt = APPEND_RETRY_DELAYS_MS.size))
        assertNull(appendRetryDelayMs(append = true, attempt = 7))
    }
}
