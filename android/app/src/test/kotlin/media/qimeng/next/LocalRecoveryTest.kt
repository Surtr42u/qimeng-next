package media.qimeng.next

import media.qimeng.next.runtime.RuntimePhase
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 连接失败横幅上的「连接本机影库」判定（纯函数，单测锁定）。
 *
 * 两条死路都要覆盖：
 * - 本机运行模块可用的安装包覆盖安装后，保存的会话仍指向一台已不可达的远端服务器；
 * - 会话已经属于本机端点，但运行层启动失败、子进程退出或卡在启动中，数据层只报通用
 *   「连接失败」而没有出口。
 *
 * 出口只应在真正连不上、本机端点未就绪，或远端传输失败时出现；本机 READY 时的业务错误
 * 与远端 HTTP 业务错误都不提供切换。
 */
class LocalRecoveryTest {

    private val runtimeUrl = "http://127.0.0.1:18431/"
    private val deadRemote = "http://192.0.2.10:8921/"

    @Test fun offersRecoveryForADeadRemoteSessionOnAnEmbeddedBuild() {
        assertTrue(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = deadRemote, runtimeBaseUrl = runtimeUrl,
            failureIsTransport = true, runtimePhase = RuntimePhase.READY, currentIsOwnedLocalEndpoint = false))
    }

    @Test fun neverOffersWithoutALocalRuntime() {
        // client 安装包没有本机运行模块，切换按钮没有落点。
        assertFalse(shouldOfferLocalRecovery(
            runtimeAvailable = false, currentBaseUrl = deadRemote, runtimeBaseUrl = runtimeUrl,
            failureIsTransport = true, runtimePhase = RuntimePhase.READY, currentIsOwnedLocalEndpoint = false))
        assertFalse(shouldOfferLocalRecovery(
            runtimeAvailable = false, currentBaseUrl = deadRemote, runtimeBaseUrl = null,
            failureIsTransport = true, runtimePhase = RuntimePhase.FAILED, currentIsOwnedLocalEndpoint = false))
    }

    @Test fun remoteBusinessFailuresNeverOfferASwitch() {
        // 401/403/404/409/5xx 与校验异常已经收到 HTTP 应答或属于业务规则；
        // 切换媒体库会掩盖真实原因，不能提供这个出口。
        assertFalse(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = deadRemote, runtimeBaseUrl = runtimeUrl,
            failureIsTransport = false, runtimePhase = RuntimePhase.READY, currentIsOwnedLocalEndpoint = false))
        assertFalse(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = deadRemote, runtimeBaseUrl = runtimeUrl,
            failureIsTransport = false, runtimePhase = RuntimePhase.FAILED, currentIsOwnedLocalEndpoint = false))
    }

    @Test fun aLocalSessionThatIsNotReadyStillOffersRecovery() {
        // 会话就是本机端点，运行层失败/停止/仍在启动：即便拿到的不是传输异常，也要给出口。
        listOf(RuntimePhase.FAILED, RuntimePhase.STOPPED, RuntimePhase.STARTING).forEach { phase ->
            assertTrue("phase=$phase", shouldOfferLocalRecovery(
                runtimeAvailable = true, currentBaseUrl = runtimeUrl, runtimeBaseUrl = runtimeUrl,
                failureIsTransport = false, runtimePhase = phase, currentIsOwnedLocalEndpoint = false))
        }
    }

    @Test fun aLocalTransportFailureOffersRecoveryEvenWhenTheRuntimeClaimsReady() {
        // READY 但请求仍连不上（子进程僵死等）：状态位不能把「连接本机影库」藏起来。
        assertTrue(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = runtimeUrl, runtimeBaseUrl = runtimeUrl,
            failureIsTransport = true, runtimePhase = RuntimePhase.READY, currentIsOwnedLocalEndpoint = false))
    }

    @Test fun aReadyLocalBusinessFailureDoesNotOfferASwitch() {
        assertFalse(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = runtimeUrl, runtimeBaseUrl = runtimeUrl,
            failureIsTransport = false, runtimePhase = RuntimePhase.READY, currentIsOwnedLocalEndpoint = false))
    }

    @Test fun neverOffersWhenTheSessionAlreadyUsesTheLocalEndpointAndIsReady() {
        // 保存的根地址可能没有尾斜杠，仍算同一个本机端点。
        assertFalse(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = "http://127.0.0.1:18431", runtimeBaseUrl = runtimeUrl,
            failureIsTransport = false, runtimePhase = RuntimePhase.READY, currentIsOwnedLocalEndpoint = false))
    }

    @Test fun offersForAStaleLocalSessionOnAnotherPortOrScheme() {
        // 用户曾把本机端口配置为 18500 后改回 18431：旧会话连不上时仍需要显式恢复。
        assertTrue(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = "http://127.0.0.1:18500/", runtimeBaseUrl = runtimeUrl,
            failureIsTransport = true, runtimePhase = RuntimePhase.READY, currentIsOwnedLocalEndpoint = true))
        // 旧版本的 https://127.0.0.1:port/ 会话在自动迁移未武装（enabled=false）时也能一键回到
        // 当前的 loopback HTTP 入口；scheme 参与比较，因此不会被当成「已经在本机」。
        assertTrue(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = "https://127.0.0.1:18431/", runtimeBaseUrl = runtimeUrl,
            failureIsTransport = true, runtimePhase = RuntimePhase.READY, currentIsOwnedLocalEndpoint = false))
    }

    @Test fun endpointOwnershipCoversAnAddressThatNoLongerMatchesTheConfiguredPort() {
        // ownedEndpoints 确认过归属时，地址比较的细节不再决定是否给出口。
        assertTrue(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = "http://127.0.0.1:18500/", runtimeBaseUrl = runtimeUrl,
            failureIsTransport = false, runtimePhase = RuntimePhase.FAILED, currentIsOwnedLocalEndpoint = true))
    }

    @Test fun neverOffersWithoutAnActiveSessionOrARuntimeAddress() {
        assertFalse(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = null, runtimeBaseUrl = runtimeUrl,
            failureIsTransport = true, runtimePhase = RuntimePhase.FAILED, currentIsOwnedLocalEndpoint = false))
        assertFalse(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = "  ", runtimeBaseUrl = runtimeUrl,
            failureIsTransport = true, runtimePhase = RuntimePhase.FAILED, currentIsOwnedLocalEndpoint = false))
        assertFalse(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = deadRemote, runtimeBaseUrl = null,
            failureIsTransport = true, runtimePhase = RuntimePhase.FAILED, currentIsOwnedLocalEndpoint = false))
        assertFalse(shouldOfferLocalRecovery(
            runtimeAvailable = true, currentBaseUrl = deadRemote, runtimeBaseUrl = "",
            failureIsTransport = true, runtimePhase = RuntimePhase.FAILED, currentIsOwnedLocalEndpoint = false))
    }

    @Test fun aLocalEndpointThatIsNotReadyRestartsTheRuntimeOnRetry() {
        // 数据层对空 loopback 端口只会再报一次 IOException；横幅的重试必须先拉起运行层。
        listOf(RuntimePhase.FAILED, RuntimePhase.STARTING, RuntimePhase.STOPPED).forEach { phase ->
            assertTrue("phase=$phase", shouldRestartLocalRuntime(
                runtimeAvailable = true, sessionIsLocalEndpoint = true, runtimePhase = phase))
        }
    }

    @Test fun aReadyLocalEndpointKeepsTheNormalListRetry() {
        assertFalse(shouldRestartLocalRuntime(
            runtimeAvailable = true, sessionIsLocalEndpoint = true, runtimePhase = RuntimePhase.READY))
        assertFalse(shouldRestartLocalRuntime(
            runtimeAvailable = true, sessionIsLocalEndpoint = true, runtimePhase = RuntimePhase.STOPPING))
        assertFalse(shouldRestartLocalRuntime(
            runtimeAvailable = true, sessionIsLocalEndpoint = true, runtimePhase = null))
    }

    @Test fun restartIsNeverOfferedForRemoteSessionsOrWithoutTheRuntimeModule() {
        assertFalse(shouldRestartLocalRuntime(
            runtimeAvailable = true, sessionIsLocalEndpoint = false, runtimePhase = RuntimePhase.FAILED))
        assertFalse(shouldRestartLocalRuntime(
            runtimeAvailable = false, sessionIsLocalEndpoint = true, runtimePhase = RuntimePhase.FAILED))
    }

    @Test fun aLocalRuntimeFailureMessageIsPreferredOverTheGenericTransportText() {
        listOf(RuntimePhase.FAILED, RuntimePhase.STARTING, RuntimePhase.STOPPED).forEach { phase ->
            assertTrue("phase=$phase", shouldPreferRuntimeFailureMessage(
                runtimeAvailable = true, sessionIsLocalEndpoint = true,
                runtimePhase = phase, runtimeMessage = "本机服务已停止（退出码 1）"))
        }
    }

    @Test fun theGenericMessageStaysWhenThereIsNoLocalRuntimeReason() {
        assertFalse(shouldPreferRuntimeFailureMessage(
            runtimeAvailable = true, sessionIsLocalEndpoint = true,
            runtimePhase = RuntimePhase.FAILED, runtimeMessage = null))
        assertFalse(shouldPreferRuntimeFailureMessage(
            runtimeAvailable = true, sessionIsLocalEndpoint = true,
            runtimePhase = RuntimePhase.FAILED, runtimeMessage = "  "))
        assertFalse(shouldPreferRuntimeFailureMessage(
            runtimeAvailable = true, sessionIsLocalEndpoint = true,
            runtimePhase = RuntimePhase.READY, runtimeMessage = "本机影库正在运行"))
        assertFalse(shouldPreferRuntimeFailureMessage(
            runtimeAvailable = true, sessionIsLocalEndpoint = false,
            runtimePhase = RuntimePhase.FAILED, runtimeMessage = "本机服务已停止（退出码 1）"))
        assertFalse(shouldPreferRuntimeFailureMessage(
            runtimeAvailable = false, sessionIsLocalEndpoint = true,
            runtimePhase = RuntimePhase.FAILED, runtimeMessage = "本机服务已停止（退出码 1）"))
    }

    @Test fun endpointComparisonIgnoresOnlyTheTrailingRootSlash() {
        assertTrue(sameEndpoint(runtimeUrl, "http://127.0.0.1:18431"))
        assertFalse(sameEndpoint(runtimeUrl, "https://127.0.0.1:18431/"))
        assertFalse(sameEndpoint(runtimeUrl, "http://127.0.0.1:18500/"))
        assertFalse(sameEndpoint(null, runtimeUrl))
        assertFalse(sameEndpoint(runtimeUrl, " "))
    }

    @Test fun aLocalServerExitOffersTheIndexRebuild() {
        // 子进程带退出码退出：控制目录里的索引数据让服务启不起来，重启运行层不会自愈。
        assertTrue(shouldOfferLocalIndexRebuild(
            runtimeAvailable = true, sessionIsLocalEndpoint = true, runtimePhase = RuntimePhase.FAILED,
            runtimeMessage = "本机程序启动失败（退出码 1）：database disk image is malformed"))
        assertTrue(shouldOfferLocalIndexRebuild(
            runtimeAvailable = true, sessionIsLocalEndpoint = true, runtimePhase = RuntimePhase.FAILED,
            runtimeMessage = "本机程序启动失败（退出码 1）"))
    }

    @Test fun theIndexRebuildNeverAppearsWithoutAServerExit() {
        // 启动超时、凭据缺失、仍在前台启动/已就绪、远端会话或没有运行模块都不提供。
        assertFalse(shouldOfferLocalIndexRebuild(
            runtimeAvailable = true, sessionIsLocalEndpoint = true, runtimePhase = RuntimePhase.FAILED,
            runtimeMessage = "本机启动超时，未取得受信回执"))
        assertFalse(shouldOfferLocalIndexRebuild(
            runtimeAvailable = true, sessionIsLocalEndpoint = true, runtimePhase = RuntimePhase.FAILED,
            runtimeMessage = null))
        listOf(RuntimePhase.READY, RuntimePhase.STARTING, RuntimePhase.STOPPING, RuntimePhase.STOPPED, null).forEach { phase ->
            assertFalse("phase=$phase", shouldOfferLocalIndexRebuild(
                runtimeAvailable = true, sessionIsLocalEndpoint = true, runtimePhase = phase,
                runtimeMessage = "本机程序启动失败（退出码 1）"))
        }
        assertFalse(shouldOfferLocalIndexRebuild(
            runtimeAvailable = true, sessionIsLocalEndpoint = false, runtimePhase = RuntimePhase.FAILED,
            runtimeMessage = "本机程序启动失败（退出码 1）"))
        assertFalse(shouldOfferLocalIndexRebuild(
            runtimeAvailable = false, sessionIsLocalEndpoint = true, runtimePhase = RuntimePhase.FAILED,
            runtimeMessage = "本机程序启动失败（退出码 1）"))
    }
}
