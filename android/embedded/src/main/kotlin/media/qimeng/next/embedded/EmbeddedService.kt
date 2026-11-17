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
import androidx.core.app.NotificationCompat
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
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
import javax.net.ssl.HttpsURLConnection

/** Owns the native process only. Database, ingestion and scan logic remain in the same Go service. */
class EmbeddedService : Service() {
    private val hostThread = Executors.newSingleThreadExecutor { task -> Thread(task, "qimeng-local-host") }
    private val main = Handler(Looper.getMainLooper())
    private val process = AtomicReference<Process?>()
    private val running = AtomicBoolean()
    private val stopping = AtomicBoolean()
    private var generation = 0L
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
        if (running.compareAndSet(false, true)) {
            HostState.ownerActive.set(true)
            stopping.set(false)
            stopWarning = null
            generation = HostState.generation.incrementAndGet()
            val ticket = generation
            HostState.status.value = RuntimeStatus(RuntimePhase.STARTING, "正在启动本机影库")
            HostState.certificateDerBase64 = null
            // PDEATHSIG is tied to the parent task. This exact thread launches AND waits for the child.
            hostThread.execute { runOwnedProcess(ticket, startId) }
        }
        return START_STICKY
    }

    private fun runOwnedProcess(ticket: Long, startId: Int) {
        var child: Process? = null
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
            if (receipt.exists() && !receipt.delete()) fail("无法清理上次本机启动回执")
            val launchId = UUID.randomUUID().toString()
            val storageRoots = EmbeddedFileAccess.roots(this, media)
            val port = secrets.port
            val listen = "127.0.0.1:$port"
            val baseUrl = addressForPort(port)
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
                put("QIMENG_EPHEMERAL_TLS", "true")
                put("LD_LIBRARY_PATH", nativeRoot.absolutePath)
            }
            if (stopping.get()) return
            HostDiagnostics.begin(listOf(token, filesDir.absolutePath, nativeRoot.absolutePath))
            child = builder.start()
            process.set(child)
            // Release drains without retaining output. Debug capture requires explicit synthetic-test
            // opt-in and stays bounded in memory; neither build logs or persists the raw stream.
            val output = child.inputStream
            val outputThread = Thread({ try { output.use { stream ->
                val bytes = ByteArray(4096)
                while (true) { val count = stream.read(bytes); if (count < 0) break; HostDiagnostics.append(bytes, count) }
            } } catch (_: java.io.IOException) { /* Process/pipe closed. */ } }, "qimeng-local-output").apply { isDaemon = true; start() }
            if (stopping.get()) child.destroy()
            val deadline = SystemClock.elapsedRealtime() + 45_000
            var instanceId: String? = null
            while (!stopping.get() && child.isAlive && SystemClock.elapsedRealtime() < deadline) {
                if (receipt.isFile) {
                    if (receipt.length() !in 1..32_768) fail("本机启动回执无效")
                    val value = JSONObject(receipt.readText())
                    if (value.getString("launchId") != launchId || value.getString("listenAddress") != listen) fail("本机启动回执与本次进程不匹配")
                    val declared = value.getString("instanceId")
                    val certificate = value.getString("certificateDerBase64")
                    if (declared.isBlank() || (secrets.confirmedInstance != null && secrets.confirmedInstance != declared)) fail("本机实例身份已改变，请先核对数据恢复结果")
                    if (httpInstance(baseUrl, certificate) != declared || !child.isAlive) fail("本机服务身份核对未通过")
                    secrets.confirm(declared, port)
                    if (HostState.generation.get() == ticket) {
                        HostState.certificateDerBase64 = certificate
                        HostState.storageRoots = storageRoots
                        HostState.activeBaseUrl = baseUrl
                    }
                    instanceId = declared
                    break
                }
                Thread.sleep(100)
            }
            if (!stopping.get()) {
                if (instanceId == null) {
                    if (!child.isAlive) outputThread.join(1_000)
                    fail(if (child.isAlive) "本机启动超时，未取得受信回执" else "本机程序启动失败（退出码 ${child.exitValue()}）")
                }
                publish(ticket, RuntimeStatus(RuntimePhase.READY, "本机影库正在运行", instanceId))
            }
            val exitCode = child.waitFor()
            if (!stopping.get()) fail("本机服务已停止（退出码 $exitCode），可重新启动核对恢复结果")
        } catch (failure: Exception) {
            if (!stopping.get()) publish(ticket, RuntimeStatus(RuntimePhase.FAILED,
                if (failure is HostFailure) failure.message.orEmpty() else "本机启动失败；原数据保留，请核对安装包和本机配置"))
        } finally {
            if (child?.isAlive == true) {
                child.destroy()
                if (!child.waitFor(25, java.util.concurrent.TimeUnit.SECONDS)) { child.destroyForcibly(); child.waitFor() }
            }
            process.compareAndSet(child, null)
            running.set(false)
            if (HostState.generation.get() == ticket) HostState.ownerActive.set(false)
            if (stopping.get()) publish(ticket, stoppedStatus())
            main.post { stopSelf(startId) }
        }
    }

    private fun binary(root: File, name: String): File {
        val file = File(root, name)
        if (file.canonicalFile.parentFile != root || !file.isFile || !file.canExecute()) fail("此安装包缺少可执行的本机程序，请安装完整本机版本")
        return file
    }

    private fun httpInstance(baseUrl: String, certificate: String): String {
        val connection = URL("${baseUrl}api/v1/instance").openConnection() as HttpsURLConnection
        connection.sslSocketFactory = PinnedTls(certificate).socketFactory
        connection.instanceFollowRedirects = false
        connection.connectTimeout = 1_500; connection.readTimeout = 1_500
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
            main.postDelayed({ if (process.get() === child && stopping.get() && child.isAlive) child.destroyForcibly() }, 25_000)
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
        process.get()?.let { child ->
            child.destroy()
            main.postDelayed({ if (process.get() === child && child.isAlive) child.destroyForcibly() }, 25_000)
        }
        // shutdown keeps the launching thread alive until its current native child has exited.
        hostThread.shutdown()
        super.onDestroy()
    }

    private fun fail(message: String): Nothing = throw HostFailure(message)
    private class HostFailure(message: String) : Exception(message)

    companion object {
        const val START = "media.qimeng.next.embedded.START"
        const val STOP = "media.qimeng.next.embedded.STOP"
        private const val CHANNEL = "local-library"
        private const val NOTIFICATION_ID = 7002
    }
}
