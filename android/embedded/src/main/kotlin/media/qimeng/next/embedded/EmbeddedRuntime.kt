package media.qimeng.next.embedded

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.TimeoutCancellationException
import media.qimeng.next.runtime.*
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicBoolean
import java.io.File
import java.net.URI
import java.util.Locale

internal object HostState {
    val status = MutableStateFlow(RuntimeStatus(RuntimePhase.STOPPED, "本机服务未运行"))
    val generation = AtomicLong()
    val ownerActive = AtomicBoolean()
    @Volatile var certificateDerBase64: String? = null
    @Volatile var storageRoots: String? = null
    @Volatile var activeBaseUrl: String? = null
}

class EmbeddedRuntime(context: Context) : LocalRuntime {
    private val app = context.applicationContext
    private val secrets = EmbeddedSecrets(app)
    private val mutex = Mutex()
    override val available = true
    override val shouldRestore: Boolean get() = secrets.enabled
    private val endpoint = MutableStateFlow(addressForPort(secrets.port))
    override val configuredAddress = endpoint.asStateFlow()
    override val baseUrl: String get() = endpoint.value
    override val status = HostState.status.asStateFlow()
    override val files = EmbeddedFileAccess(app)

    override suspend fun start(): LocalConnection = mutex.withLock {
        repeat(2) {
        files.refreshAccess()
        val desiredRoots = withContext(Dispatchers.IO) { EmbeddedFileAccess.roots(app, File(app.filesDir, "embedded-v1/media")) }
        if ((status.value.phase == RuntimePhase.READY && (HostState.storageRoots != desiredRoots || HostState.activeBaseUrl != baseUrl)) ||
            status.value.phase == RuntimePhase.STOPPING ||
            (status.value.phase == RuntimePhase.FAILED && HostState.ownerActive.get())) stopLocked()
        withContext(Dispatchers.IO) { secrets.enabled = true }
        if (status.value.phase != RuntimePhase.READY) {
            HostState.status.value = RuntimeStatus(RuntimePhase.STARTING, "正在启动本机影库")
            try { ContextCompat.startForegroundService(app, Intent(app, EmbeddedService::class.java).setAction(EmbeddedService.START)) }
            catch (failure: Exception) {
                HostState.status.value = RuntimeStatus(RuntimePhase.FAILED, "系统未允许启动本机前台服务，请回到应用重试")
                throw IllegalStateException("系统未允许启动本机前台服务", failure)
            }
        }
        val ready = try { withTimeout(55_000) { status.first { it.phase == RuntimePhase.READY || it.phase == RuntimePhase.FAILED || it.phase == RuntimePhase.STOPPED } } }
        catch (failure: TimeoutCancellationException) {
            app.stopService(Intent(app, EmbeddedService::class.java))
            HostState.status.value = RuntimeStatus(RuntimePhase.FAILED, "本机服务启动超时，已停止本次启动")
            throw IllegalStateException("本机服务启动超时", failure)
        }
        check(ready.phase == RuntimePhase.READY) { ready.message }
        if (HostState.activeBaseUrl != baseUrl || HostState.storageRoots != desiredRoots) {
            stopLocked()
            return@repeat
        }
        val instance = requireNotNull(ready.instanceId)
        val token = withContext(Dispatchers.IO) { secrets.token(allowCreate = false) }
        return@withLock LocalConnection(requireNotNull(HostState.activeBaseUrl), instance, token, requireNotNull(HostState.certificateDerBase64) { "本机TLS启动凭据缺失" })
        }
        error("本机权限或地址仍在变化，请稍后重新连接")
    }

    override suspend fun stop() = mutex.withLock { stopLocked() }

    private suspend fun stopLocked() {
        withContext(Dispatchers.IO) { secrets.enabled = false }
        if (status.value.phase in setOf(RuntimePhase.STOPPED, RuntimePhase.FAILED) && !HostState.ownerActive.get()) return
        app.startService(Intent(app, EmbeddedService::class.java).setAction(EmbeddedService.STOP))
        withTimeout(30_000) { status.first { !HostState.ownerActive.get() && it.phase in setOf(RuntimePhase.STOPPED, RuntimePhase.FAILED) } }
        Unit
    }

    override suspend fun confirmInstance(instanceId: String) = withContext(Dispatchers.IO) { secrets.confirm(instanceId) }

    override suspend fun configureAddress(address: String) = mutex.withLock {
        val uri = try { URI(address.trim()) } catch (_: Exception) { throw IllegalArgumentException("请输入完整的本机回环地址和端口") }
        require(uri.scheme in setOf("http", "https") && uri.host?.lowercase(Locale.ROOT) in setOf("localhost", "127.0.0.1") &&
            uri.rawUserInfo == null && uri.rawQuery == null && uri.rawFragment == null && uri.path.orEmpty() in setOf("", "/")) { "本机地址必须是回环根地址，不能包含凭据或路径" }
        withContext(Dispatchers.IO) { secrets.port = uri.port }
        endpoint.value = addressForPort(uri.port)
    }
    override fun ownedEndpoints(instanceId: String): Set<String> = if (secrets.confirmedInstance == instanceId) secrets.confirmedPorts().map(::addressForPort).toSet() else emptySet()
}

internal fun addressForPort(port: Int) = "https://127.0.0.1:$port/"
