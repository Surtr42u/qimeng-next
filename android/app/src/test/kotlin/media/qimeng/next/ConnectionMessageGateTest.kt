package media.qimeng.next

import media.qimeng.next.data.ApiFailure
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException
import java.net.SocketTimeoutException

/**
 * 「连接失败」提示冷却闸门（用户反馈：横幅在正常使用中反复出现）。
 *
 * 后台健康检查、自动恢复与详情补数据会连续撞上同一个断线窗口；同一文本在屏上或
 * 4s 内刚发过就不再重发。成功/用户清除后 [ConnectionMessageGate.reset] 让下一次
 * 真实失败仍能出现。
 */
class ConnectionMessageGateTest {

    private val text = "连接失败，请检查服务器后重试"
    private val other = "连接失败，请重新登录此连接"

    @Test fun nonConnectionMessagesAreNeverCoalesced() {
        val gate = ConnectionMessageGate()
        assertTrue(gate.shouldPost("请求失败（HTTP 404）", null, 0L))
        assertTrue(gate.shouldPost("请求失败（HTTP 404）", null, 1L))
        assertTrue(gate.shouldPost("请输入服务器密码", "请求失败（HTTP 404）", 2L))
        assertTrue(gate.shouldPost("还是同一条业务错误", "还是同一条业务错误", 3L))
        // 冷却只针对连接失败文本，业务错误没有时间窗口。
        assertTrue(gate.shouldPost("操作未完成，请重试", null, 4L))
    }

    @Test fun identicalMessageAlreadyOnScreenIsKeptInsteadOfReposted() {
        val gate = ConnectionMessageGate()
        assertTrue(gate.shouldPost(text, null, 0L))
        assertFalse(gate.shouldPost(text, text, 1L))
        assertFalse(gate.shouldPost(text, text, 60_000L))
    }

    @Test fun identicalMessageInsideTheFourSecondCooldownIsNotReposted() {
        val gate = ConnectionMessageGate()
        assertTrue(gate.shouldPost(text, null, 0L))
        // 横幅已被别的提示覆盖，但同一失败在冷却窗内不重发。
        assertFalse(gate.shouldPost(text, "请求失败（HTTP 404）", 1L))
        assertFalse(gate.shouldPost(text, null, 3_999L))
        // 恰好到窗口即可再次提示（严格小于才抑制）。
        assertTrue(gate.shouldPost(text, null, 4_000L))
    }

    @Test fun aDifferentConnectionFailureIsNotSuppressedByAnotherOne() {
        val gate = ConnectionMessageGate()
        assertTrue(gate.shouldPost(text, null, 0L))
        // 换了一条不同的连接失败文本：立即提示，不被前一条的冷却挡住。
        assertTrue(gate.shouldPost(other, null, 1L))
        // 再回到第一条文本：它已不是「刚发过的同一文本」，照常提示。
        assertTrue(gate.shouldPost(text, null, 2L))
        // 同一文本连续出现才受冷却约束。
        assertFalse(gate.shouldPost(text, null, 3L))
    }

    @Test fun resetRestoresPromptingAfterASuccessPath() {
        val gate = ConnectionMessageGate()
        assertTrue(gate.shouldPost(text, null, 0L))
        assertFalse(gate.shouldPost(text, null, 1_000L))
        gate.reset()
        assertTrue("成功清除提示后，下一次真实失败必须能出现", gate.shouldPost(text, null, 1_001L))
    }

    @Test fun pureDecisionMatchesGateStateMachine() {
        // visible 优先于冷却：已在屏上时无论多久都不重发。
        assertFalse(shouldPostConnectionMessage(text, text, text, 0L, 500_000L))
        // 从未发布过同一文本。
        assertTrue(shouldPostConnectionMessage(text, null, null, null, 0L))
        assertTrue(shouldPostConnectionMessage(text, null, other, 1_000L, 1_001L))
        // 冷却边界。
        assertFalse(shouldPostConnectionMessage(text, null, text, 1_000L, 1_000L + CONNECTION_MESSAGE_COOLDOWN_MS - 1))
        assertTrue(shouldPostConnectionMessage(text, null, text, 1_000L, 1_000L + CONNECTION_MESSAGE_COOLDOWN_MS))
        assertTrue(shouldPostConnectionMessage("请求失败（HTTP 500）", null, text, 1_000L, 1_001L))
        assertTrue("前缀之外的文本不受连接冷却影响", shouldPostConnectionMessage("连接已改变，请重新登录", null, text, 1_000L, 1_001L))
    }

    @Test fun transportFailuresAreSeparatedFromHttpAnswers() {
        assertTrue(isTransportFailure(IOException("connection reset")))
        assertTrue(isTransportFailure(SocketTimeoutException("read timed out")))
        // ApiFailure 是已经收到的 HTTP 应答，不能被当成连接问题静默/重试。
        assertFalse(isTransportFailure(ApiFailure(404, "对象不存在或已被移除")))
        assertFalse(isTransportFailure(ApiFailure(401, "登录已失效，请重新登录")))
        assertFalse(isTransportFailure(IllegalStateException("连接已改变")))
    }
}
