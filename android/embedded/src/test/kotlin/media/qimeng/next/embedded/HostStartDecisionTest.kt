package media.qimeng.next.embedded

import media.qimeng.next.runtime.RuntimePhase
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 僵尸服务守卫（纯判断，单测锁定）：host 任务已结束后，前台 START / HEALTH_CHECK 必须能
 * 重新拉起，而不是把 `running=true` 当成“已在运行”永远不再启动。
 */
class HostStartDecisionTest {

    @Test fun anIdleServiceStartsAHost() {
        assertTrue(shouldStartHost(phase = RuntimePhase.STOPPED, stopping = false, hostOwnerActive = false, hostLoopActive = false))
        assertTrue(shouldStartHost(phase = RuntimePhase.FAILED, stopping = false, hostOwnerActive = false, hostLoopActive = false))
        assertTrue(shouldStartHost(phase = RuntimePhase.READY, stopping = false, hostOwnerActive = false, hostLoopActive = false))
    }

    @Test fun aFinishedHostWithStaleRunningFlagIsRevived() {
        // 被覆盖的 startId 让旧 host 的 stopSelf 失效：服务仍在、没有任务；new START 必须重拉。
        assertTrue(shouldStartHost(phase = RuntimePhase.FAILED, stopping = false, hostOwnerActive = false, hostLoopActive = false))
        assertTrue(shouldStartHost(phase = RuntimePhase.STOPPED, stopping = false, hostOwnerActive = false, hostLoopActive = false))
    }

    @Test fun theAppSideStartTimeoutMarkerDoesNotHideALostHost() {
        // start() 先把状态改成 STARTING 再发 intent；此时若 host 任务已丢失，仍必须能重拉。
        assertTrue(shouldStartHost(phase = RuntimePhase.STARTING, stopping = false, hostOwnerActive = false, hostLoopActive = false))
        assertTrue(shouldStartHost(phase = null, stopping = false, hostOwnerActive = false, hostLoopActive = false))
    }

    @Test fun aQueuedOrRunningHostIsNotDuplicated() {
        // hostLoopActive=true 覆盖“已排队但尚未真正执行”的窗口，避免两个 host 各起一个子进程。
        assertFalse(shouldStartHost(phase = RuntimePhase.STARTING, stopping = false, hostOwnerActive = true, hostLoopActive = true))
        assertFalse(shouldStartHost(phase = RuntimePhase.FAILED, stopping = false, hostOwnerActive = true, hostLoopActive = true))
        assertFalse(shouldStartHost(phase = RuntimePhase.READY, stopping = false, hostOwnerActive = true, hostLoopActive = true))
    }

    @Test fun aTaskStillHoldingTheTicketDuringStopBlocksASecondLaunch() {
        // 卡在原生/Keystore/进程启动的任务仍持有启动标号：不能在同一实例里再起一个。
        assertFalse(shouldStartHost(phase = RuntimePhase.STARTING, stopping = true, hostOwnerActive = true, hostLoopActive = false))
        assertFalse(shouldStartHost(phase = RuntimePhase.STOPPED, stopping = true, hostOwnerActive = true, hostLoopActive = false))
    }

    @Test fun aStoppingServiceIsNotRevivedMidTransition() {
        assertFalse(shouldStartHost(phase = RuntimePhase.STOPPING, stopping = false, hostOwnerActive = false, hostLoopActive = false))
        assertFalse(shouldStartHost(phase = RuntimePhase.STOPPING, stopping = true, hostOwnerActive = false, hostLoopActive = false))
    }

    @Test fun anExplicitStartAfterACompletedStopCanRestart() {
        // stop() 已完成且没有 host 任务持有标号；新 START（只有用户点按的 retryLocal 会走到
        // 这里）可以复用本实例。HEALTH_CHECK 在 stopping 时已由 onStartCommand 提前返回。
        assertTrue(shouldStartHost(phase = RuntimePhase.STOPPED, stopping = true, hostOwnerActive = false, hostLoopActive = false))
        assertTrue(shouldStartHost(phase = RuntimePhase.FAILED, stopping = true, hostOwnerActive = false, hostLoopActive = false))
        assertTrue(shouldStartHost(phase = RuntimePhase.STARTING, stopping = true, hostOwnerActive = false, hostLoopActive = false))
    }
}
