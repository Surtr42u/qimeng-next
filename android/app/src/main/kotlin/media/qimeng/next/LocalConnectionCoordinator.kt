package media.qimeng.next

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import media.qimeng.next.data.DataGraph
import media.qimeng.next.runtime.LocalRuntime
import media.qimeng.next.runtime.RuntimePhase

/** App composition only: native lifecycle and API sessions meet here, never inside a Repository. */
class LocalConnectionCoordinator(private val graph: DataGraph, private val runtime: LocalRuntime, scope: CoroutineScope) {
    private val mutex = Mutex()
    private val mutableError = MutableStateFlow<String?>(null)
    val error = mutableError.asStateFlow()

    init {
        if (runtime.available) scope.launch {
            runtime.status.collect { status ->
                if (status.phase == RuntimePhase.READY && runtime.shouldRestore && graph.settings.session.value?.baseUrl == runtime.baseUrl) {
                    try { connect(forceLogin = false) }
                    catch (cancelled: CancellationException) { throw cancelled }
                    catch (_: Exception) { mutableError.value = "本机服务已启动，但客户端连接尚未恢复，请重新连接" }
                }
            }
        }
    }

    suspend fun connect(forceLogin: Boolean, password: String = "") = mutex.withLock {
        if (!forceLogin && !runtime.shouldRestore) return@withLock
        mutableError.value = null
        val selectedBeforeStart = graph.settings.session.value
        val connection = runtime.start()
        val current = graph.settings.session.value
        if (current != selectedBeforeStart) return@withLock
        if (forceLogin || current == null || current.baseUrl != connection.baseUrl || current.instanceId != connection.instanceId ||
            graph.settings.certificateFor(current) != connection.certificateDerBase64) {
            if (password.isEmpty()) graph.media.login(connection.baseUrl, connection.bootstrapToken, connection.certificateDerBase64, connection.instanceId)
            else graph.media.loginWithPassword(connection.baseUrl, password, connection.certificateDerBase64, connection.instanceId, rememberAddress = false)
        }
        runtime.confirmInstance(connection.instanceId)
        graph.uploads.schedule()
    }

    suspend fun restoreSelected() {
        if (runtime.available && runtime.shouldRestore && graph.settings.session.value?.baseUrl == runtime.baseUrl) connect(forceLogin = false)
    }
}
