package media.qimeng.next.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * 认证客户端传输层重试的纯判定（旧版问题：「一次瞬时 IOException 立刻弹连接失败」）。
 *
 * 只补幂等读（GET/HEAD）最多 2 次（300ms、900ms）；写操作、SSE 与取消一律不重试。
 * 非 2xx 是应答不是异常，本判定不参与（由拦截器只捕获 IOException 保证）。
 */
class TransportRetryPolicyTest {

    @Test fun idempotentReadsWaitThreeHundredThenNineHundredMilliseconds() {
        assertEquals(listOf(300L, 900L), TRANSPORT_RETRY_DELAYS_MS.toList())
        assertEquals(300L, transportRetryDelayMs(method = "GET", accept = "application/json", cancelled = false, attempt = 0))
        assertEquals(900L, transportRetryDelayMs(method = "GET", accept = "application/json", cancelled = false, attempt = 1))
        assertNull("只允许 2 次补试", transportRetryDelayMs(method = "GET", accept = null, cancelled = false, attempt = 2))
        assertNull(transportRetryDelayMs(method = "GET", accept = null, cancelled = false, attempt = 7))
    }

    @Test fun headIsRetriedLikeGet() {
        assertEquals(300L, transportRetryDelayMs(method = "HEAD", accept = null, cancelled = false, attempt = 0))
        assertEquals(900L, transportRetryDelayMs(method = "HEAD", accept = null, cancelled = false, attempt = 1))
    }

    @Test fun writeMethodsAreNeverRetried() {
        for (method in listOf("POST", "PUT", "PATCH", "DELETE")) {
            assertNull(method, transportRetryDelayMs(method = method, accept = "application/json", cancelled = false, attempt = 0))
            assertNull(method, transportRetryDelayMs(method = method, accept = null, cancelled = false, attempt = 1))
        }
    }

    @Test fun eventStreamIsOwnedByItsOwnBackoff() {
        assertNull(transportRetryDelayMs(method = "GET", accept = "text/event-stream", cancelled = false, attempt = 0))
        // 头值大小写/空白差异同样视为 SSE。
        assertNull(transportRetryDelayMs(method = "GET", accept = " Text/Event-Stream ", cancelled = false, attempt = 0))
        // 组合 Accept 里带 SSE 也不能重试：消费者已有自己的重连链。
        assertNull(transportRetryDelayMs(method = "GET", accept = "application/json, text/event-stream", cancelled = false, attempt = 0))
    }

    @Test fun cancellationNeverRetries() {
        assertNull(transportRetryDelayMs(method = "GET", accept = null, cancelled = true, attempt = 0))
        assertNull(transportRetryDelayMs(method = "HEAD", accept = null, cancelled = true, attempt = 1))
    }
}
