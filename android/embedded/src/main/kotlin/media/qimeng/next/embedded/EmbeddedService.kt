package media.qimeng.next.embedded

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.SystemClock
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import media.qimeng.next.runtime.RuntimePhase
import media.qimeng.next.runtime.RuntimeStatus
import media.qimeng.next.transport.PinnedTls
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.TimeZone
import java.util.UUID
import java.util.concurrent.Executors
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.locks.ReentrantLock
import javax.net.ssl.HttpsURLConnection

/** Owns the native process only. Database, ingestion and scan logic remain in the same Go service. */
class EmbeddedService : Service() {
    private val hostThread = Executors.newSingleThreadExecutor { task -> Thread(task, "qimeng-local-host") }
    private val main = Handler(Looper.getMainLooper())

    // Health checks block on loopback HTTP and on the recheck window, so they never run on the
    // main thread or on the host thread that is blocked in Process.waitFor.
    private val watchdogScheduler: ScheduledExecutorService = Executors.newSingleThreadScheduledExecutor { task ->
        Thread(task, "qimeng-local-watchdog").apply { isDaemon = true }
    }
    private val process = AtomicReference<Process?>()
    private val session = AtomicReference<HostSession?>()
    private val watchdog = AtomicReference<WatchdogHandle?>()
    private val restartReason = AtomicReference<String?>(null)
    private val running = AtomicBoolean()
    private val hostOwner = AtomicLong()
    private val stopping = AtomicBoolean()
    private val healthGate = ReentrantLock()
    private val healthRequestPending = AtomicBoolean()
    private var sessionEpoch = 0L
    @Volatile private var stopWarning: String? = null

    override fun onCreate() {
        super.onCreate()
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel(CHANNEL, "本机影库", NotificationManager.IMPORTANCE_LOW))
        val notification = notification()
        if (Build.VERSION.SDK_INT >= 34) startForeground(NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        else startForeground(NOTIFICATION_ID, notification)
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == STOP || !EmbeddedSecrets(this).enabled) {
            if (intent?.action == STOP) {
                try { EmbeddedSecrets(this).enabled = false }
                catch (_: Exception) { stopWarning = "本机服务已停止，但无法保存停止偏好，请重试设置" }
            }
            stopOwnedProcess(startId)
            return START_NOT_STICKY
        }
        if (intent?.action == HEALTH_CHECK) {
            // A foreground return asks an already running service for one probe. When the service
            // survived but no host task is running, the same intent falls through to a normal start.
            if (stopping.get()) return START_NOT_STICKY
            if (running.compareAndSet(false, true)) beginHost(startId)
            else requestHealthProbe()
            return START_STICKY
        }
        if (running.compareAndSet(false, true)) beginHost(startId)
        return START_STICKY
    }

    private fun beginHost(startId: Int) {
        HostState.ownerActive.set(true)
        stopping.set(false)
        stopWarning = null
        val ticket = HostState.generation.incrementAndGet()
        HostState.status.value = RuntimeStatus(RuntimePhase.STARTING, "正在启动本机影库")
        HostState.certificateDerBase64 = null
        hostOwner.set(ticket)
        // PDEATHSIG is tied to the parent task. This exact thread launches AND waits for the child.
        hostThread.execute { runOwnedProcess(ticket, startId) }
    }

    /**
     * Host loop. Each iteration is one native child launch; a health-triggered kill returns from
     * [Process.waitFor] with [restartReason] set and launches the next attempt in place. Natural
     * exits still end in FAILED/stopSelf, matching the previous host's no-crash-loop decision.
     */
    private fun runOwnedProcess(ticket: Long, startId: Int) {
        var child: Process? = null
        var attempt = 0
        try {
            val secrets = EmbeddedSecrets(this)
            val root = File(filesDir.canonicalFile, "embedded-v1")
            val control = File(root, "control")
            val media = File(root, "media")
            val runtime = File(root, "runtime")
            if (secrets.confirmedInstance != null && !control.isDirectory) fail("原本机数据目录不可用，已停止启动以免创建空库")
            val emptyControl = !control.exists() || control.list()?.isEmpty() == true
            val token = secrets.token(allowCreate = emptyControl)
            listOf(control, media, runtime).forEach { directory ->
                if ((!directory.isDirectory && !directory.mkdirs()) || directory.canonicalFile != directory.absoluteFile) fail("无法准备本机私有目录")
            }
            val nativeRoot = File(applicationInfo.nativeLibraryDir).canonicalFile
            val runner = binary(nativeRoot, "libqimeng_runner.so")
            val server = binary(nativeRoot, "libqimeng.so")
            val receipt = File(runtime, "ready.json")
            val storageRoots = EmbeddedFileAccess.roots(this, media)
            val port = secrets.port
            val listen = "127.0.0.1:$port"
            val baseUrl = addressForPort(port)
            while (true) {
                attempt += 1
                restartReason.set(null)
                if (stopping.get()) return
                if (receipt.exists() && !receipt.delete()) fail("无法清理上次本机启动回执")
                val launchId = UUID.randomUUID().toString()
                val builder = ProcessBuilder(runner.absolutePath, server.absolutePath).directory(control).redirectErrorStream(true)
                builder.environment().apply {
                    keys.filter { it.startsWith("QIMENG_") }.toList().forEach(::remove)
                    put("QIMENG_LISTEN", listen)
                    put("QIMENG_DATA_DIR", control.absolutePath)
                    put("QIMENG_STORAGE_ROOTS", storageRoots)
                    put("QIMENG_TIMEZONE", TimeZone.getDefault().id)
                    put("QIMENG_ADMIN_TOKEN", token)
                    put("QIMENG_FFMPEG", File(nativeRoot, "libffmpeg_cli.so").absolutePath)
                    put("QIMENG_FFPROBE", File(nativeRoot, "libffprobe_cli.so").absolutePath)
                    put("QIMENG_STARTUP_RECEIPT", receipt.absolutePath)
                    put("QIMENG_LAUNCH_ID", launchId)
                    put("LD_LIBRARY_PATH", nativeRoot.absolutePath)
                }
                if (stopping.get()) return
                HostDiagnostics.begin(listOf(token, filesDir.absolutePath, nativeRoot.absolutePath))
                val launched = builder.start()
                child = launched
                process.set(launched)
                sessionEpoch += 1
                val epoch = sessionEpoch
                val startedAt = SystemClock.elapsedRealtime()
                // Release drains without retaining output. Debug capture requires explicit synthetic-test
                // opt-in and stays bounded in memory; neither build logs or persists the raw stream.
                val output = launched.inputStream
                val outputThread = Thread({ try { output.use { stream ->
                    val bytes = ByteArray(4096)
                    while (true) { val count = stream.read(bytes); if (count < 0) break; HostDiagnostics.append(bytes, count) }
                } } catch (_: java.io.IOException) { /* Process/pipe closed. */ } }, "qimeng-local-output").apply { isDaemon = true; start() }
                if (stopping.get()) launched.destroy()
                val deadline = startedAt + STARTUP_RECEIPT_TIMEOUT_MS
                var instanceId: String? = null
                var certificate = ""
                while (!stopping.get() && launched.isAlive && SystemClock.elapsedRealtime() < deadline) {
                    if (receipt.isFile) {
                        if (receipt.length() !in 1..32_768) fail("本机启动回执无效")
                        val value = JSONObject(receipt.readText())
                        if (value.getString("launchId") != launchId || value.getString("listenAddress") != listen) fail("本机启动回执与本次进程不匹配")
                        val declared = value.getString("instanceId")
                        certificate = value.optString("certificateDerBase64")
                        if (declared.isBlank() || (secrets.confirmedInstance != null && secrets.confirmedInstance != declared)) fail("本机实例身份已改变，请先核对数据恢复结果")
                        if (httpInstance(baseUrl, certificate) != declared || !launched.isAlive) fail("本机服务身份核对未通过")
                        secrets.confirm(declared, port)
                        if (HostState.generation.get() == ticket) {
                            HostState.certificateDerBase64 = certificate.ifBlank { null }
                            HostState.storageRoots = storageRoots
                            HostState.activeBaseUrl = baseUrl
                        }
                        instanceId = declared
                        break
                    }
                    Thread.sleep(100)
                }
                if (!stopping.get()) {
                    val readyInstance = instanceId ?: run {
                        if (!launched.isAlive) outputThread.join(1_000)
                        fail(if (launched.isAlive) "本机启动超时，未取得受信回执" else "本机程序启动失败（退出码 ${launched.exitValue()}）")
                    }
                    val active = HostSession(ticket, epoch, launched, baseUrl, certificate, readyInstance, startedAt, attempt)
                    session.set(active)
                    publish(ticket, RuntimeStatus(RuntimePhase.READY, "本机影库正在运行", readyInstance))
                    Log.i(LOG_TAG, "local server ready attempt=$attempt epoch=$epoch uptimeMs=${SystemClock.elapsedRealtime() - startedAt}")
                    scheduleWatchdog(active)
                }
                val exitCode = launched.waitFor()
                if (stopping.get()) return
                runCatching { outputThread.join(1_000) }
                val reason = restartReason.getAndSet(null)
                cancelWatchdog(launched)
                val uptimeMs = SystemClock.elapsedRealtime() - startedAt
                if (reason == null) fail("本机服务已停止（退出码 $exitCode），可重新启动核对恢复结果")
                Log.i(LOG_TAG, "local server exited for health restart reason=$reason attempt=$attempt epoch=$epoch uptimeMs=$uptimeMs")
                session.set(null)
                process.compareAndSet(launched, null)
                child = null
                publish(ticket, RuntimeStatus(RuntimePhase.STARTING, "本机影库正在恢复（第 ${attempt + 1} 次启动）"))
            }
        } catch (failure: Exception) {
            if (!stopping.get()) publish(ticket, RuntimeStatus(RuntimePhase.FAILED,
                if (failure is HostFailure) failure.message.orEmpty() else "本机启动失败；原数据保留，请核对安装包和本机配置"))
        } finally {
            session.set(null)
            watchdog.getAndSet(null)?.future?.cancel(false)
            val finished = child
            if (finished != null && finished.isAlive) {
                finished.destroy()
                if (!finished.waitFor(STOP_GRACE_MS, TimeUnit.MILLISECONDS)) { finished.destroyForcibly(); finished.waitFor() }
            }
            process.compareAndSet(finished, null)
            running.set(false)
            // A newer host may already own the service after running became false; only the
            // current owner may clear the shared flag.
            hostOwner.compareAndSet(ticket, 0L)
            if (HostState.generation.get() == ticket) HostState.ownerActive.set(false)
            if (stopping.get()) publish(ticket, stoppedStatus())
            main.post { stopSelf(startId) }
        }
    }

    /**
     * Watchdog tick: every [WATCHDOG_INTERVAL_MS] ask the liveness endpoint whether the child is
     * still scheduling. A frozen process keeps its listening socket open but answers nothing,
     * which [Process.isAlive] cannot detect.
     *
     * Ticks are scheduled one at a time. While Android freezes the app the task cannot run; the
     * queued tick runs once after thaw and then resumes the fixed cadence. A foreground request
     * covers the same case when the service itself was frozen with the child.
     */
    private fun scheduleWatchdog(active: HostSession) {
        if (watchdogScheduler.isShutdown) return
        val tick = object : Runnable {
            override fun run() {
                if (!isCurrentSession(active) || !active.child.isAlive) return
                val restarted = verifyChildHealth(WATCHDOG_REASON)
                if (!restarted && isCurrentSession(active) && active.child.isAlive && !watchdogScheduler.isShutdown) {
                    watchdogScheduler.schedule(this, WATCHDOG_INTERVAL_MS, TimeUnit.MILLISECONDS)
                }
            }
        }
        val future = runCatching {
            watchdogScheduler.schedule(tick, WATCHDOG_INTERVAL_MS, TimeUnit.MILLISECONDS)
        }.getOrNull() ?: return
        watchdog.set(WatchdogHandle(active, future))
    }

    /** Foreground return from the app shell; collapses repeated onStart requests while one runs. */
    private fun requestHealthProbe() {
        if (!healthRequestPending.compareAndSet(false, true)) return
        try {
            watchdogScheduler.execute {
                try { verifyChildHealth(FOREGROUND_REASON) } finally { healthRequestPending.set(false) }
            }
        } catch (_: RejectedExecutionException) {
            healthRequestPending.set(false)
        }
    }

    /**
     * @return true when the captured child was killed for a health restart.
     */
    private fun verifyChildHealth(reason: String): Boolean {
        if (stopping.get() || HostState.status.value.phase != RuntimePhase.READY) return false
        val active = session.get()
        if (active == null || !active.child.isAlive) {
            ensureHostStarted()
            return false
        }
        if (!isCurrentSession(active)) return false
        return ChildHealthRecovery(
            gate = healthGate,
            current = { process.get() },
            alive = { it.isAlive },
            ensureStarted = { ensureHostStarted() },
            probe = { probeHealth(active) },
            pauseBeforeRecheck = {
                try { Thread.sleep(HEALTH_RECHECK_DELAY_MS) }
                catch (_: InterruptedException) { Thread.currentThread().interrupt() }
            },
            restart = { observed, why -> restartUnhealthyChild(active, observed, why) },
        ).runOnce(reason)
    }

    /**
     * Cheap unauthenticated loopback liveness probe, answered from memory and verified against
     * the instance identity captured at startup. It never reads media, sends a request body, or
     * touches SQLite.
     *
     * /healthz is not used here: its handler calls CheckReadiness, which pings SQLite. A 5 s
     * watchdog must not touch the database (a long write would look like a freeze). The public
     * instance document is already used for the startup identity check and is answered from
     * memory. Matching the id also prevents a foreign listener on the same port from being
     * accepted as healthy after our listener dies.
     */
    private fun probeHealth(active: HostSession): Boolean = runCatching {
        httpInstance(active.baseUrl, active.certificate, HEALTH_PROBE_TIMEOUT_MS) == active.instanceId
    }.getOrDefault(false)

    /**
     * Kills the captured child and lets the host loop relaunch it. The watchdog is cancelled
     * only after the identity check, so an older check can never cancel a newer child's watchdog.
     * SIGKILL is intentional: a process whose scheduler stopped will not run SIGTERM handlers.
     */
    private fun restartUnhealthyChild(active: HostSession, child: Process, reason: String) {
        if (!isCurrentSession(active) || process.get() !== child || !child.isAlive || stopping.get()) return
        val uptimeMs = SystemClock.elapsedRealtime() - active.startedAt
        Log.w(LOG_TAG, "local server health check failed twice; restarting reason=$reason attempt=${active.attempt} epoch=${active.epoch} uptimeMs=$uptimeMs")
        restartReason.compareAndSet(null, reason)
        cancelWatchdog(active)
        runCatching { child.destroyForcibly() }
    }

    private fun ensureHostStarted() {
        if (stopping.get() || hostOwner.get() != 0L || !EmbeddedSecrets(this).enabled) return
        // Foreground-only call site: the platform rejects an FGS start from a background shell.
        runCatching {
            ContextCompat.startForegroundService(this, Intent(this, EmbeddedService::class.java).setAction(START))
        }
    }

    private fun isCurrentSession(active: HostSession): Boolean =
        !stopping.get() && running.get() && HostState.generation.get() == active.ticket &&
            session.get() === active && process.get() === active.child

    /** Identity-checked cancellation: a stale check never cancels the watchdog of a newer child. */
    private fun cancelWatchdog(active: HostSession) {
        val handle = watchdog.get() ?: return
        if (handle.session === active && watchdog.compareAndSet(handle, null)) handle.future.cancel(false)
    }

    private fun cancelWatchdog(child: Process) {
        val handle = watchdog.get() ?: return
        if (handle.session.child === child && watchdog.compareAndSet(handle, null)) handle.future.cancel(false)
    }

    private fun binary(root: File, name: String): File {
        val file = File(root, name)
        if (file.canonicalFile.parentFile != root || !file.isFile || !file.canExecute()) fail("此安装包缺少可执行的本机程序，请安装完整本机版本")
        return file
    }

    private fun httpInstance(baseUrl: String, certificate: String, timeoutMs: Int = IDENTITY_TIMEOUT_MS): String {
        val connection = URL("$baseUrl$INSTANCE_PATH").openConnection() as HttpURLConnection
        if (connection is HttpsURLConnection && certificate.isNotBlank()) {
            connection.sslSocketFactory = PinnedTls(certificate).socketFactory
        }
        connection.instanceFollowRedirects = false
        connection.useCaches = false
        connection.connectTimeout = timeoutMs; connection.readTimeout = timeoutMs
        try {
            if (connection.responseCode != 200) fail("本机服务尚未返回有效实例信息")
            val output = ByteArrayOutputStream()
            connection.inputStream.use { input ->
                val bytes = ByteArray(512)
                while (output.size() <= 4096) {
                    val count = input.read(bytes, 0, minOf(bytes.size, 4097 - output.size()))
                    if (count < 0) break
                    output.write(bytes, 0, count)
                }
            }
            if (output.size() > 4096) fail("本机实例响应超过允许大小")
            return JSONObject(output.toString("UTF-8")).getString("id")
        } finally { connection.disconnect() }
    }

    private fun stopOwnedProcess(startId: Int) {
        stopping.set(true)
        HostState.status.value = RuntimeStatus(RuntimePhase.STOPPING, "正在保存并停止本机服务")
        val child = process.get()
        child?.destroy()
        if (!running.get()) {
            HostState.status.value = stoppedStatus()
            stopSelf(startId)
        } else if (child != null) {
            main.postDelayed({ if (process.get() === child && stopping.get() && child.isAlive) child.destroyForcibly() }, STOP_GRACE_MS)
        }
    }

    private fun publish(ticket: Long, state: RuntimeStatus) { if (HostState.generation.get() == ticket) HostState.status.value = state }
    private fun stoppedStatus() = stopWarning?.let { RuntimeStatus(RuntimePhase.FAILED, it) }
        ?: RuntimeStatus(RuntimePhase.STOPPED, "本机服务已停止")

    private fun notification(): Notification {
        val stop = PendingIntent.getService(this, 1, Intent(this, EmbeddedService::class.java).setAction(STOP), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        val launch = packageManager.getLaunchIntentForPackage(packageName)?.let { PendingIntent.getActivity(this, 0, it, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE) }
        return NotificationCompat.Builder(this, CHANNEL).setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentTitle("绮梦本机影库").setContentText("正在处理本机影库请求，数据保存在本应用内")
            .setContentIntent(launch).setOngoing(true).addAction(android.R.drawable.ic_media_pause, "停止本机服务", stop).build()
    }

    override fun onDestroy() {
        stopping.set(true)
        session.set(null)
        watchdog.getAndSet(null)?.future?.cancel(false)
        watchdogScheduler.shutdownNow()
        process.get()?.let { child ->
            child.destroy()
            main.postDelayed({ if (process.get() === child && child.isAlive) child.destroyForcibly() }, STOP_GRACE_MS)
        }
        // shutdown keeps the launching thread alive until its current native child has exited.
        hostThread.shutdown()
        super.onDestroy()
    }

    private fun fail(message: String): Nothing = throw HostFailure(message)
    private class HostFailure(message: String) : Exception(message)

    private class HostSession(
        val ticket: Long,
        val epoch: Long,
        val child: Process,
        val baseUrl: String,
        val certificate: String,
        val instanceId: String,
        val startedAt: Long,
        val attempt: Int,
    )
    private class WatchdogHandle(val session: HostSession, val future: ScheduledFuture<*>)

    companion object {
        const val START = "media.qimeng.next.embedded.START"
        const val STOP = "media.qimeng.next.embedded.STOP"
        const val HEALTH_CHECK = "media.qimeng.next.embedded.HEALTH_CHECK"
        private const val CHANNEL = "local-library"
        private const val NOTIFICATION_ID = 7002
        private const val LOG_TAG = "QimengEmbedded"
        private const val INSTANCE_PATH = "api/v1/instance"

        /** Startup identity request budget; the probe reuses the same request with its own timeout. */
        private const val IDENTITY_TIMEOUT_MS = 1_500
        private const val WATCHDOG_REASON = "watchdog"
        private const val FOREGROUND_REASON = "foreground"

        /** Startup receipt budget; the Go server writes it after it is able to answer. */
        private const val STARTUP_RECEIPT_TIMEOUT_MS = 45_000L

        /** Grace for the Go server's 20 s shutdown before the host force-kills it. */
        private const val STOP_GRACE_MS = 25_000L

        /** Measured by the previous host: 3 s without a loopback response means scheduling stopped. */
        private const val HEALTH_PROBE_TIMEOUT_MS = 3_000

        /** Measured by the previous host: window between the two probes of one decision. */
        private const val HEALTH_RECHECK_DELAY_MS = 2_000L

        /** Measured by the previous host: liveness cadence that detects freeze without busy polling. */
        private const val WATCHDOG_INTERVAL_MS = 5_000L
    }
}
