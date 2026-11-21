package media.qimeng.next

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 统计页惰性加载闸门（重复进入不再重取五组接口，回前台按窗口补读）。
 *
 * 只在两种情况下跳过：本范围报告仍在 [ANALYTICS_FRESHNESS_MS] 内且无错误，或本范围请求
 * 已在途。范围变化、从未加载、窗口过期、上次失败都必须立即请求；在途的是别的范围时也
 * 必须改取当前范围，否则旧请求落地后页面会停在别的范围的报告上（渲染按
 * `overview.range == requestedRange` 过滤），没有请求再补。错误卡的重试不经过本闸门，
 * 直接调 `loadAnalytics` 强制刷新。
 */
class AnalyticsLoadGateTest {

    private val loadedAt = 1_000L

    private fun load(
        loadedRange: String? = null,
        loadedAtMs: Long? = null,
        error: String? = null,
        inFlightRange: String? = null,
        requestedRange: String = "7d",
        nowMs: Long = loadedAt,
    ) = shouldLoadAnalytics(loadedRange, loadedAtMs, error, inFlightRange, requestedRange, nowMs)

    @Test fun idleRangeAlwaysLoads() {
        // 从未取到报告、没有错误、没有在途请求：每个范围都要请求。
        assertTrue(load(requestedRange = "7d"))
        assertTrue(load(requestedRange = "30d"))
        assertTrue(load(requestedRange = "all"))
    }

    @Test fun aFreshReportForTheRequestedRangeIsNotReloaded() {
        // 本范围报告在窗口内：重进标签页不重取、卡片不闪。
        assertFalse(load(loadedRange = "7d", loadedAtMs = loadedAt, requestedRange = "7d", nowMs = loadedAt))
        assertFalse(load(loadedRange = "30d", loadedAtMs = loadedAt, requestedRange = "30d", nowMs = loadedAt + 59_999))
        assertFalse(load(loadedRange = "all", loadedAtMs = loadedAt, requestedRange = "all", nowMs = loadedAt + ANALYTICS_FRESHNESS_MS - 1))
    }

    @Test fun aReportOlderThanTheWindowIsReloaded() {
        // 恰好到窗口就视为过期（判定是严格小于）；再久同样要补读。
        assertTrue(load(loadedRange = "7d", loadedAtMs = loadedAt, requestedRange = "7d", nowMs = loadedAt + ANALYTICS_FRESHNESS_MS))
        assertTrue(load(loadedRange = "30d", loadedAtMs = loadedAt, requestedRange = "30d", nowMs = loadedAt + ANALYTICS_FRESHNESS_MS * 10))
    }

    @Test fun switchingTheRangeLoadsImmediately() {
        // 换范围必须立刻取新范围，不能因为手上有一份别的范围的报告就跳过。
        assertTrue(load(loadedRange = "7d", loadedAtMs = loadedAt, requestedRange = "30d"))
        assertTrue(load(loadedRange = "30d", loadedAtMs = loadedAt, requestedRange = "7d"))
        assertTrue(load(loadedRange = "all", loadedAtMs = loadedAt, requestedRange = "7d"))
    }

    @Test fun anInFlightRequestForTheSameRangeIsNotCancelledOrRestarted() {
        assertFalse(load(inFlightRange = "7d", requestedRange = "7d"))
        // 即便旧报告属于别的范围、或已经过期，同一范围的请求在途也不重启。
        assertFalse(load(loadedRange = "30d", loadedAtMs = loadedAt, inFlightRange = "7d",
            requestedRange = "7d", nowMs = loadedAt + ANALYTICS_FRESHNESS_MS * 2))
        // 错误标志不会让同一范围的在途请求被取消重来。
        assertFalse(load(loadedRange = "7d", loadedAtMs = loadedAt, error = "连接失败",
            inFlightRange = "7d", requestedRange = "7d"))
    }

    @Test fun anInFlightRequestForAnotherRangeYieldsToTheRequestedRange() {
        // 先取 30d 又切回 7d：最后选择的范围获胜，即便 7d 手上还有一份新鲜报告。
        assertTrue(load(loadedRange = "7d", loadedAtMs = loadedAt, inFlightRange = "30d", requestedRange = "7d"))
        assertTrue(load(loadedRange = "7d", loadedAtMs = loadedAt, error = "连接失败",
            inFlightRange = "30d", requestedRange = "7d"))
        assertTrue(load(inFlightRange = "all", requestedRange = "30d"))
    }

    @Test fun aFailedRequestIsRetriedEvenWhenTheRangeMatches() {
        // 上次失败（无论是否还留着窗口内的旧报告）：重新请求，错误卡也另有显式重试。
        assertTrue(load(error = "连接失败", requestedRange = "7d"))
        assertTrue(load(loadedRange = "7d", loadedAtMs = loadedAt, error = "连接失败", requestedRange = "7d"))
    }

    @Test fun aStaleReportForAnotherRangeNeverSuppressesTheRequest() {
        assertTrue(load(loadedRange = "7d", loadedAtMs = loadedAt, requestedRange = "all"))
        assertTrue(load(loadedRange = "all", loadedAtMs = loadedAt, error = "连接失败", requestedRange = "30d"))
    }

    @Test fun aReportWithoutATimestampIsNotAssumedFresh() {
        // 没有落地时间（例如状态来自别处）时无从判断新鲜度，宁可请求。
        assertTrue(load(loadedRange = "7d", loadedAtMs = null, requestedRange = "7d"))
    }
}
