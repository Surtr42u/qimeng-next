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
import media.qimeng.next.data.DataGraph
import media.qimeng.next.runtime.LocalRuntime
import media.qimeng.next.runtime.RuntimePhase
import media.qimeng.next.model.Session

/** App composition only: native lifecycle and API sessions meet here, never inside a Repository. */
class LocalConnectionCoordinator(private val graph: DataGraph, private val runtime: LocalRuntime, private val scope: CoroutineScope) {
    private val mutex = Mutex()
    private val mutableError = MutableStateFlow<String?>(null)
    val error = mutableError.asStateFlow()

    init {
        if (runtime.available) scope.launch {
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
    suspend fun connect(forceLogin: Boolean, password: String = "") = scope.async {
        try { mutex.withLock { connectLocked(forceLogin, password) } }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) {
            mutableError.value = "本机连接恢复流程尚未完成，请查看传输状态并重试连接"
            throw failure
        }
    }.await()

    private suspend fun connectLocked(forceLogin: Boolean, password: String) {
        if (!forceLogin && !runtime.shouldRestore) return
        mutableError.value = null
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
