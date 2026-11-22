package media.qimeng.next.runtime

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

enum class RuntimePhase { STOPPED, STARTING, READY, STOPPING, FAILED }
data class RuntimeStatus(val phase: RuntimePhase, val message: String, val instanceId: String? = null)

/** A short-lived handoff to normal API session exchange; deliberately not a data class. */
class LocalConnection(val baseUrl: String, val instanceId: String, val bootstrapToken: String, val certificateDerBase64: String?) {
    override fun toString() = "LocalConnection(instance=$instanceId, credential=redacted)"
}

interface LocalRuntime {
    val available: Boolean
    val shouldRestore: Boolean
    val baseUrl: String?
    val configuredAddress: StateFlow<String>
    val status: StateFlow<RuntimeStatus>
    val files: LocalFiles
    val scans: LocalScans
    suspend fun start(): LocalConnection
    suspend fun stop()

    /**
     * Foreground self-heal hook: an embedded runtime probes its owned child and replaces a child
     * that is alive but no longer scheduling. Runtimes without an owned process do nothing.
     */
    fun requestHealthCheck() = Unit

    /**
     * Rebuild the local catalog index without touching media: an embedded runtime renames the
     * confirmed control directory to a backup, forgets the instance identity and starts a fresh
     * instance. The returned connection uses a new instance identity, so the caller must log in
     * again before normal API requests. Runtimes without a local service reject the request.
     */
    suspend fun rebuildLocalIndex(): LocalConnection = throw UnsupportedOperationException("此安装包未包含本机服务")

    suspend fun confirmInstance(instanceId: String)
    suspend fun configureAddress(address: String)
    fun validateAddress(address: String): String
    fun ownedEndpoints(instanceId: String): Set<String>
}

object RemoteOnlyRuntime : LocalRuntime {
    override val available = false
    override val shouldRestore = false
    override val baseUrl: String? = null
    override val configuredAddress = MutableStateFlow("")
    override val status = MutableStateFlow(RuntimeStatus(RuntimePhase.STOPPED, "此安装包仅支持连接服务器"))
    override val files = UnavailableLocalFiles
    override val scans = UnavailableLocalScans
    override suspend fun start(): LocalConnection = error("此安装包未包含本机服务")
    override suspend fun stop() = Unit
    override suspend fun confirmInstance(instanceId: String) = Unit
    override suspend fun configureAddress(address: String) = error("此安装包未包含本机服务")
    override fun validateAddress(address: String): String = error("此安装包未包含本机服务")
    override fun ownedEndpoints(instanceId: String): Set<String> = emptySet()
}
