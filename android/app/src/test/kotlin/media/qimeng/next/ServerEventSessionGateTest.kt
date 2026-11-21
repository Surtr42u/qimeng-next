package media.qimeng.next

import media.qimeng.next.model.Session
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 服务器推送与会话的配对（§5 的登出/换服保护）。
 *
 * 事件流每次建立都会把计数各加一；会话刚建立时那一次不能拿来重读旧会话的列表。
 * 同一会话上的后续计数才是真实推送。注意 Session 是数据类：同一实例/同一连接算同一会话。
 */
class ServerEventSessionGateTest {
    private val server = Session("http://192.0.2.10:8420", "instance-a", "NAS")
    private val other = Session("http://192.0.2.11:8420", "instance-b", "备用")

    @Test fun theFirstCounterAfterLoginIsTreatedAsAReconnectAndSkipped() {
        val gate = ServerEventSessionGate()
        assertFalse(gate.accept(server))
    }

    @Test fun laterCountersOnTheSameSessionAreRealPushes() {
        val gate = ServerEventSessionGate()
        gate.accept(server)
        assertTrue(gate.accept(server))
        assertTrue(gate.accept(server))
    }

    @Test fun logoutNeverAcceptsAndASubsequentLoginIsSkippedAgain() {
        val gate = ServerEventSessionGate()
        gate.accept(server)
        assertTrue(gate.accept(server))
        // 登出：连接关闭时没有计数，登出后的计数若到达也不得重读。
        assertFalse(gate.accept(null))
        // 重新登录（可能是同一台服务器）：流重连的第一次计数仍只算重连。
        assertFalse(gate.accept(server))
        assertTrue(gate.accept(server))
    }

    @Test fun switchingServersDoesNotCarryTheOldSessionApproval() {
        val gate = ServerEventSessionGate()
        gate.accept(server)
        assertTrue(gate.accept(server))
        assertFalse(gate.accept(other))
        assertTrue(gate.accept(other))
    }

    @Test fun aSessionChangeForgetsEvenTheSameServerApproval() {
        // 登出后再登录同一台服务器：会话切换本身已清空列表，流重连的第一次计数不得补读。
        val gate = ServerEventSessionGate()
        gate.accept(server)
        assertTrue(gate.accept(server))
        gate.forget()
        assertFalse(gate.accept(server))
        assertTrue(gate.accept(server))
    }

    @Test fun twoSessionsThatCompareEqualAreTheSameSession() {
        val gate = ServerEventSessionGate()
        val copy = server.copy()
        gate.accept(server)
        assertTrue(gate.accept(copy))
    }
}
