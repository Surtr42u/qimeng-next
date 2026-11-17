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

internal object HostState {
    const val BASE_URL = "https://127.0.0.1:18431/"
    const val LISTEN = "127.0.0.1:18431"
    val status = MutableStateFlow(RuntimeStatus(RuntimePhase.STOPPED, "本机服务未运行"))
    val generation = AtomicLong()
    val ownerActive = AtomicBoolean()
    @Volatile var certificateDerBase64: String? = null
    @Volatile var storageRoots: String? = null
}

class EmbeddedRuntime(context: Context) : LocalRuntime {
    private val app = context.applicationContext
    private val secrets = EmbeddedSecrets(app)
    private val mutex = Mutex()
    override val available = true
    override val shouldRestore: Boolean get() = secrets.enabled
    override val baseUrl: String = HostState.BASE_URL
    override val status = HostState.status.asStateFlow()
    override val files = EmbeddedFileAccess(app)

    override suspend fun start(): LocalConnection = mutex.withLock {
        files.refreshAccess()
        val desiredRoots = withContext(Dispatchers.IO) { EmbeddedFileAccess.roots(app, File(app.filesDir, "embedded-v1/media")) }
        if ((status.value.phase == RuntimePhase.READY && HostState.storageRoots != desiredRoots) ||
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
        val instance = requireNotNull(ready.instanceId)
        val token = withContext(Dispatchers.IO) { secrets.token(allowCreate = false) }
        LocalConnection(HostState.BASE_URL, instance, token, requireNotNull(HostState.certificateDerBase64) { "本机TLS启动凭据缺失" })
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
}
