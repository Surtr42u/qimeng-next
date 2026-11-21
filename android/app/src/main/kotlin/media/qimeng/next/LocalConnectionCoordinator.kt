package media.qimeng.next

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull
import media.qimeng.next.data.DataGraph
import media.qimeng.next.runtime.LocalRuntime
import media.qimeng.next.runtime.RuntimePhase
import media.qimeng.next.model.Session

/** 显式「重启本机影库」时等待旧 host 退出的上限；超时后仍按正常流程再启动一次。 */
private const val LOCAL_RESTART_STOP_TIMEOUT_MS = 15_000L

/** App composition only: native lifecycle and API sessions meet here, never inside a Repository. */
class LocalConnectionCoordinator(private val graph: DataGraph, private val runtime: LocalRuntime, private val scope: CoroutineScope) {
    private val mutex = Mutex()
    private val mutableError = MutableStateFlow<String?>(null)
    val error = mutableError.asStateFlow()

    init {
        if (runtime.available) scope.launch {
            // 旧版本本机地址是 https://127.0.0.1:port/；升级到回环 HTTP 后自动迁移一次，
            // 避免用户停留在“连接失败”而必须手动切一次本机模式。
            val selected = graph.settings.session.value
            if (runtime.shouldRestore && selected?.baseUrl?.startsWith("https://127.0.0.1:") == true) {
                try { connect(forceLogin = true) }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (_: Exception) { mutableError.value = "本机连接地址升级未完成，请重试连接本机影库" }
            }
            runtime.status.collect { status ->
                if (status.phase == RuntimePhase.READY && runtime.shouldRestore && graph.settings.session.value?.baseUrl == runtime.baseUrl) {
                    try { connect(forceLogin = false) }
                    catch (cancelled: CancellationException) { throw cancelled }
                    catch (_: Exception) { mutableError.value = "本机连接恢复流程尚未完成，请查看传输状态并重试连接" }
                }
            }
        }
    }

    // The selected-session flow cancels the originating ViewModel action. This application-owned
    // operation must still complete the authenticated journal handoff after saving that session.
    suspend fun connect(forceLogin: Boolean, password: String = "", retryLocal: Boolean = false) = scope.async {
        try { mutex.withLock { connectLocked(forceLogin, password, retryLocal) } }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) {
            mutableError.value = "本机连接恢复流程尚未完成，请查看传输状态并重试连接"
            throw failure
        }
    }.await()

    private suspend fun connectLocked(forceLogin: Boolean, password: String, retryLocal: Boolean) {
        // 自动恢复尊重用户显式停止（shouldRestore=false）；失败横幅上的点按是显式重试，
        // 即使此前停止过也要重新拉起运行层，但仍按原会话登录、不强制重新登录。
        if (!forceLogin && !retryLocal && !runtime.shouldRestore) return
        mutableError.value = null
        if (retryLocal && runtime.status.value.phase == RuntimePhase.STARTING) {
            // 用户点按的「重启本机影库」遇到卡在 STARTING 的旧 host：有界地先停止，让 stopOwnedProcess
            // 收尾并 stopSelf，再按正常流程启动。自动恢复不带 retryLocal，不会打断正常的冷启动。
            withTimeoutOrNull(LOCAL_RESTART_STOP_TIMEOUT_MS) { runtime.stop() }
        }
        val selectedBeforeStart = graph.settings.session.value
        val connection = runtime.start()
        val current = graph.settings.session.value
        if (current != selectedBeforeStart) return
        if (forceLogin || current == null || current.baseUrl != connection.baseUrl || current.instanceId != connection.instanceId ||
            graph.settings.certificateFor(current) != connection.certificateDerBase64) {
            if (password.isEmpty()) graph.media.login(connection.baseUrl, connection.bootstrapToken, connection.certificateDerBase64, connection.instanceId,
                loginEndpoint = if (forceLogin) "local" else null)
            else graph.media.loginWithPassword(connection.baseUrl, password, connection.certificateDerBase64, connection.instanceId, rememberAddress = false,
                loginEndpoint = if (forceLogin) "local" else null)
        }
        runtime.confirmInstance(connection.instanceId)
        if (graph.queue.state.value == null) graph.queue.load()
        graph.queue.clearConnectionProblem()
        val selected = graph.settings.session.value
        val target = Session(connection.baseUrl, connection.instanceId,
            selected?.takeIf { it.instanceId == connection.instanceId && it.baseUrl == connection.baseUrl }?.label ?: "本机影库")
        graph.queue.rebindEndpoint(target, runtime.ownedEndpoints(connection.instanceId))
        graph.uploads.schedule()
    }

    suspend fun restoreSelected() {
        if (runtime.available && runtime.shouldRestore && graph.settings.session.value?.baseUrl == runtime.baseUrl) connect(forceLogin = false)
    }
}
