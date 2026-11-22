package media.qimeng.next.embedded

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.NonCancellable
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
    override val scans: LocalScans = EmbeddedScans(app)

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
        scans.resume()
        return@withLock LocalConnection(requireNotNull(HostState.activeBaseUrl), instance, token, HostState.certificateDerBase64)
        }
        error("本机权限或地址仍在变化，请稍后重新连接")
    }

    override suspend fun stop() = mutex.withLock { stopLocked() }

    /**
     * 重建本机索引：先停止自己持有的 host，把已确认的控制目录改名为同父目录下的备份，
     * 清除实例身份（保留 Keystore 引导凭据与启用标记），再启动一个全新实例。
     *
     * 媒体目录与原件不动；新控制目录会生成新的 instanceId，调用方必须按本机登录流程重新
     * 交换会话。改名失败时运行层保持停止并恢复原启用标记；任何情况下都不删除旧目录。
     */
    override suspend fun rebuildLocalIndex(): LocalConnection {
        mutex.withLock {
            val wasEnabled = secrets.enabled
            try {
                // 停止失败（超时）时仍持有数据库：绝不能改名控制目录，直接按失败返回。
                stopLocked()
                // 改名、建空目录与清除实例身份必须整段完成：中途被取消会留下「旧目录已备份、
                // 身份仍在」的不一致状态，下一次启动反而会被数据目录守卫拦住。
                withContext(NonCancellable + Dispatchers.IO) {
                    val root = File(app.filesDir.canonicalFile, "embedded-v1")
                    val control = File(root, "control")
                    if (control.exists()) {
                        val backup = File(root, "control.bak-${System.currentTimeMillis()}")
                        check(control.renameTo(backup)) { "原本机索引目录无法改名备份，未重建本机索引" }
                    }
                    check(control.isDirectory || control.mkdirs()) { "无法创建新的本机索引目录" }
                    // 新目录从空索引开始；引导凭据与启用标记保留，媒体目录不动。
                    secrets.resetIdentity()
                }
            } catch (failure: Exception) {
                // stopLocked() 已清掉启用标记；失败时恢复原偏好，旧目录仍可按正常流程自动恢复。
                withContext(NonCancellable + Dispatchers.IO) {
                    try { if (wasEnabled) secrets.enabled = true } catch (_: Exception) { /* 保持停止态 */ }
                }
                throw failure
            }
        }
        return start()
    }

    /**
     * Returning to the foreground asks the service for one liveness probe. The service is the
     * only owner of the child, so a frozen-but-alive process can be replaced there without this
     * layer touching the process handle. An explicit user stop clears [EmbeddedSecrets.enabled]
     * and must not be resurrected by a foreground return.
     */
    override fun requestHealthCheck() {
        if (!secrets.enabled) return
        runCatching {
            ContextCompat.startForegroundService(app, Intent(app, EmbeddedService::class.java).setAction(EmbeddedService.HEALTH_CHECK))
        }
    }

    private suspend fun stopLocked() {
        withContext(Dispatchers.IO) { secrets.enabled = false }
        if (status.value.phase in setOf(RuntimePhase.STOPPED, RuntimePhase.FAILED) && !HostState.ownerActive.get()) return
        app.startService(Intent(app, EmbeddedService::class.java).setAction(EmbeddedService.STOP))
        withTimeout(30_000) { status.first { !HostState.ownerActive.get() && it.phase in setOf(RuntimePhase.STOPPED, RuntimePhase.FAILED) } }
        Unit
    }

    override suspend fun confirmInstance(instanceId: String) = withContext(Dispatchers.IO) { secrets.confirm(instanceId) }

    override suspend fun configureAddress(address: String) = mutex.withLock {
        val normalized = validateAddress(address)
        withContext(Dispatchers.IO) { secrets.port = URI(normalized).port }
        endpoint.value = normalized
    }

    override fun validateAddress(address: String): String {
        val uri = try { URI(address.trim()) } catch (_: Exception) { throw IllegalArgumentException("请输入完整的本机回环地址和端口") }
        require(uri.scheme in setOf("http", "https") && uri.host?.lowercase(Locale.ROOT) in setOf("localhost", "127.0.0.1") &&
            uri.rawUserInfo == null && uri.rawQuery == null && uri.rawFragment == null && uri.path.orEmpty() in setOf("", "/")) { "本机地址必须是回环根地址，不能包含凭据或路径" }
        require(uri.port in 1024..65535) { "本机端口需要在 1024–65535 之间" }
        return addressForPort(uri.port)
    }
    override fun ownedEndpoints(instanceId: String): Set<String> = if (secrets.confirmedInstance == instanceId) secrets.confirmedPorts().map(::addressForPort).toSet() else emptySet()
}

internal fun addressForPort(port: Int) = "http://127.0.0.1:$port/"
