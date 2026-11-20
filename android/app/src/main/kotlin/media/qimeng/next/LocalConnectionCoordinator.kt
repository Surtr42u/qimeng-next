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
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.data.DataGraph
import media.qimeng.next.runtime.LocalRuntime
import media.qimeng.next.runtime.RuntimePhase
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget

/** App composition only: native lifecycle and API sessions meet here, never inside a Repository. */
class LocalConnectionCoordinator(private val graph: DataGraph, private val runtime: LocalRuntime, private val scope: CoroutineScope) {
    private val mutex = Mutex()
    private val mutableError = MutableStateFlow<String?>(null)
    val error = mutableError.asStateFlow()
    private val mutableAdmitted = MutableStateFlow<Session?>(null)
    val admitted = mutableAdmitted.asStateFlow()

    // A PC/NAS redirected through a local port is not an embedded runtime.
    fun owns(target: Session): Boolean = runtime.available && target.baseUrl in runtime.ownedEndpoints(target.instanceId)

    fun permits(target: Session): Boolean = !owns(target) ||
        (runtime.status.value.phase == RuntimePhase.READY && sameTarget(target, admitted.value))

    init {
        if (runtime.available) scope.launch {
            runtime.status.collect { status ->
                if (status.phase != RuntimePhase.READY) mutableAdmitted.value = null
                if (status.phase == RuntimePhase.FAILED && graph.settings.session.value?.let(::owns) == true) {
                    mutableError.value = status.message
                }
                // The native host can also restart while this Activity is already foreground.
                // A READY transition can repair once; failures do not schedule retry loops.
                if (status.phase == RuntimePhase.READY && graph.settings.session.value?.let(::owns) == true) launch {
                    try { connect(forceLogin = false, onlyIfUnadmitted = true) }
                    catch (cancelled: CancellationException) { throw cancelled }
                    catch (_: Exception) { /* The coordinator retained the visible failure. */ }
                }
            }
        }
    }

    // The selected-session flow can cancel the originating ViewModel action. The application owns
    // the single-flight authenticated journal handoff, including a same-URL TLS rotation.
    suspend fun connect(forceLogin: Boolean, password: String = "") =
        connect(forceLogin, password, onlyIfUnadmitted = false)

    private suspend fun connect(forceLogin: Boolean, password: String = "", onlyIfUnadmitted: Boolean) = scope.async {
        mutex.withLock {
            val selected = graph.settings.session.value
            if (!forceLogin && (selected == null || !owns(selected) || !runtime.shouldRestore)) return@withLock
            if (onlyIfUnadmitted && selected != null && permits(selected)) return@withLock
            if (forceLogin) mutableAdmitted.value = null
            mutableError.value = null
            try {
                connectLocked(forceLogin, password, selected)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (failure: Exception) {
                if (graph.settings.session.value == selected || forceLogin) {
                    mutableAdmitted.value = null
                    mutableError.value = if (runtime.status.value.phase == RuntimePhase.FAILED) runtime.status.value.message
                        else "本机影库暂未连接。原数据和待传进度已保留，请重试。"
                }
                throw failure
            }
        }
    }.await()

    suspend fun restoreSelected() = connect(forceLogin = false)

    private suspend fun connectLocked(forceLogin: Boolean, password: String, selectedBeforeStart: Session?) {
        val connection = runtime.start()
        val current = graph.settings.session.value
        if (current != selectedBeforeStart) return
        if (!forceLogin) check(current?.instanceId == connection.instanceId) { "本机实例身份已改变" }

        suspend fun login() {
            if (password.isEmpty()) graph.media.login(connection.baseUrl, connection.bootstrapToken, connection.certificateDerBase64, connection.instanceId,
                loginEndpoint = if (forceLogin) "local" else null)
            else graph.media.loginWithPassword(connection.baseUrl, password, connection.certificateDerBase64, connection.instanceId, rememberAddress = false,
                loginEndpoint = if (forceLogin) "local" else null)
        }
        if (forceLogin || current == null || current.baseUrl != connection.baseUrl ||
            graph.settings.certificateFor(current) != connection.certificateDerBase64) login()
        else try { graph.media.verifySession(current) }
        catch (failure: ApiFailure) {
            if (failure.status != 401) throw failure
            login()
        }

        val target = graph.settings.session.value ?: return
        if (target.baseUrl != connection.baseUrl || target.instanceId != connection.instanceId) return
        runtime.confirmInstance(connection.instanceId)
        if (graph.queue.state.value == null) graph.queue.load()
        graph.queue.rebindEndpoint(target, runtime.ownedEndpoints(connection.instanceId))
        if (!sameTarget(target, graph.settings.session.value) || runtime.status.value.phase != RuntimePhase.READY) return
        mutableAdmitted.value = target
        graph.queue.clearTransportProblem(target)
        graph.uploads.schedule()
        graph.resumeAdmittedLocalWork(target)
    }
}
