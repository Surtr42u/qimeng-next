package media.qimeng.next.data

import media.qimeng.next.model.Session
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 就绪探针的纯判定（旧版 core/network/ServerReadinessProbe 口径）：
 * 300ms 轮询、10s 预算、5s 就绪短窗复用、单飞共享。
 *
 * 关键回归：新鲜度与在途探测都按「服务器地址 + 实例」分键——5s 内从 A 切到 B
 * 不能拿 A 的就绪结论回答 B。
 */
class ServerReadinessPolicyTest {

    private val serverA = Session("https://a.example/", "instance-a", "A")
    private val serverB = Session("https://b.example/", "instance-b", "B")
    private val sameAddressOtherInstance = Session("https://a.example/", "instance-a2", "A")

    @Test fun readinessIsFreshOnlyInsideTheFiveSecondWindow() {
        val key = readinessKey(serverA)
        assertTrue(ServerReadinessPolicy.isReadyFresh(key, key, 10_000L, 10_000L))
        assertTrue(ServerReadinessPolicy.isReadyFresh(key, key, 10_000L, 14_999L))
        // 恰好到窗口即过期（严格小于）。
        assertFalse(ServerReadinessPolicy.isReadyFresh(key, key, 10_000L, 15_000L))
        assertFalse(ServerReadinessPolicy.isReadyFresh(key, key, 10_000L, 15_001L))
        assertEquals(5_000L, ServerReadinessPolicy.READY_FRESH_MS)
    }

    @Test fun neverProbedIsNotFresh() {
        assertFalse(ServerReadinessPolicy.isReadyFresh(null, readinessKey(serverA), null, 1_000L))
        assertFalse(ServerReadinessPolicy.isReadyFresh(readinessKey(serverA), readinessKey(serverA), null, 1_000L))
        assertFalse(ServerReadinessPolicy.isReadyFresh(null, readinessKey(serverA), 1_000L, 1_000L))
    }

    @Test fun freshnessIsKeyedByServerNotJustTime() {
        val markKey = readinessKey(serverA)
        // 另一个地址，或同地址另一个实例，都必须重新探测。
        assertFalse(ServerReadinessPolicy.isReadyFresh(markKey, readinessKey(serverB), 10_000L, 11_000L))
        assertFalse(ServerReadinessPolicy.isReadyFresh(markKey, readinessKey(sameAddressOtherInstance), 10_000L, 11_000L))
        assertTrue(ServerReadinessPolicy.isReadyFresh(markKey, readinessKey(serverA), 10_000L, 11_000L))
        assertNotEquals(markKey, readinessKey(serverB))
        assertEquals(readinessKey(serverA), readinessKey(Session(serverA.baseUrl, serverA.instanceId, "renamed")))
    }

    @Test fun aClockThatMovedBackwardsIsNotFresh() {
        assertFalse(ServerReadinessPolicy.isReadyFresh(readinessKey(serverA), readinessKey(serverA), 10_000L, 9_000L))
    }

    @Test fun pollingStopsAtTheBudgetBoundary() {
        assertTrue(ServerReadinessPolicy.shouldKeepPolling(0L, 10_000L))
        assertTrue(ServerReadinessPolicy.shouldKeepPolling(9_999L, 10_000L))
        assertFalse(ServerReadinessPolicy.shouldKeepPolling(10_000L, 10_000L))
        assertFalse(ServerReadinessPolicy.shouldKeepPolling(10_001L, 10_000L))
        assertEquals(300L, ServerReadinessPolicy.POLL_INTERVAL_MS)
        assertEquals(10_000L, ServerReadinessPolicy.BUDGET_MS)
    }

    @Test fun onlyAnActiveProbeForTheSameServerIsShared() {
        val key = readinessKey(serverA)
        assertTrue(ServerReadinessPolicy.reuseInFlight(key, key, inFlightActive = true))
        assertFalse("已完成但尚未清引用的探测不复用", ServerReadinessPolicy.reuseInFlight(key, key, inFlightActive = false))
        assertFalse("在途探测没在跑时不能算共享", ServerReadinessPolicy.reuseInFlight(readinessKey(serverB), key, inFlightActive = false))
    }

    @Test fun aProbeForAnotherServerIsNeverShared() {
        // 用户从 A 切到 B：B 必须自己探一次，不能 await A 的结果。
        assertFalse(ServerReadinessPolicy.reuseInFlight(readinessKey(serverA), readinessKey(serverB), inFlightActive = true))
        assertFalse(ServerReadinessPolicy.reuseInFlight(readinessKey(serverB), readinessKey(serverA), inFlightActive = true))
        assertFalse(ServerReadinessPolicy.reuseInFlight(readinessKey(sameAddressOtherInstance), readinessKey(serverA), inFlightActive = true))
    }

    @Test fun clientErrorsAreTerminalButServerAndTransportErrorsKeepPolling() {
        assertTrue(ServerReadinessPolicy.isTerminalProbeStatus(400))
        assertTrue(ServerReadinessPolicy.isTerminalProbeStatus(404))
        assertTrue(ServerReadinessPolicy.isTerminalProbeStatus(499))
        assertFalse(ServerReadinessPolicy.isTerminalProbeStatus(399))
        assertFalse("5xx 是起机暂态，继续等", ServerReadinessPolicy.isTerminalProbeStatus(500))
        assertFalse(ServerReadinessPolicy.isTerminalProbeStatus(503))
    }
}
