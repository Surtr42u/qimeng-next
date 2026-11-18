package media.qimeng.next.data

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.net.wifi.WifiManager
import android.os.Build
import android.os.Handler
import android.os.Looper
import androidx.annotation.RequiresApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import media.qimeng.next.model.ServerCandidate
import media.qimeng.next.model.ServerDiscoveryState
import java.util.concurrent.Executor

/** Foreground, bounded discovery. The supplied saved-address probe is anonymous and uses the SDK. */
class ServerDiscovery(context: Context, private val probe: suspend (String) -> ServerCandidate?) {
    private val app = context.applicationContext
    private val manager = app.getSystemService(NsdManager::class.java)
    private val handler = Handler(Looper.getMainLooper())
    private val executor = Executor { handler.post(it) }
    private val mutableState = MutableStateFlow(ServerDiscoveryState())
    val state = mutableState.asStateFlow()
    private var generation = 0L
    private var listener: NsdManager.DiscoveryListener? = null
    private var multicast: WifiManager.MulticastLock? = null
    private val services = linkedMapOf<String, List<ServerCandidate>>()
    private val closers = mutableMapOf<String, () -> Unit>()
    private val legacyPending = ArrayDeque<Pair<String, NsdServiceInfo>>()
    private var legacyResolving = false
    private var expiry: Runnable? = null
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var probeJob: Job? = null

    fun start(remembered: List<String>) { handler.post { begin(remembered) } }
    fun stop() { handler.post { stopInternal() } }

    private fun begin(remembered: List<String>) {
        if (mutableState.value.searching) return
        stopInternal()
        services.clear()
        mutableState.value = ServerDiscoveryState(searching = true)
        val ticket = generation
        probeJob = scope.launch {
            for (address in remembered.distinct().take(5)) {
                val found = probe(address)
                if (ticket != generation) return@launch
                if (found != null) { services["saved:$address"] = listOf(found); publish() }
            }
            if (ticket == generation) beginNsd(ticket)
        }
    }

    private fun beginNsd(ticket: Long) {
        val callbacks = object : NsdManager.DiscoveryListener {
            override fun onDiscoveryStarted(type: String) = Unit
            override fun onDiscoveryStopped(type: String) = Unit
            override fun onStartDiscoveryFailed(type: String, errorCode: Int) = post(ticket) { finish("未能自动找到影库，可重新寻找或手动填写地址") }
            override fun onStopDiscoveryFailed(type: String, errorCode: Int) = post(ticket) {
                finish("系统尚未停止上次发现，请稍后重新寻找")
            }
            override fun onServiceFound(info: NsdServiceInfo) = post(ticket) {
                if (info.serviceType.trimEnd('.') != "_qimeng._tcp" || services.size >= 32) return@post
                val key = info.serviceName
                if (services.containsKey(key)) return@post
                services[key] = emptyList()
                if (Build.VERSION.SDK_INT >= 34) resolveCurrent(key, info, ticket)
                else { legacyPending.addLast(key to info); resolveNextLegacy(ticket) }
            }
            override fun onServiceLost(info: NsdServiceInfo) = post(ticket) {
                services.remove(info.serviceName)
                closers.remove(info.serviceName)?.invoke()
                legacyPending.removeAll { it.first == info.serviceName }
                publish()
            }
        }
        listener = callbacks
        try {
            if (Build.VERSION.SDK_INT <= 33) {
                multicast = app.getSystemService(WifiManager::class.java)?.createMulticastLock("qimeng-discovery")?.apply {
                    setReferenceCounted(false); acquire()
                }
            }
            manager.discoverServices("_qimeng._tcp.", NsdManager.PROTOCOL_DNS_SD, callbacks)
            expiry = Runnable { if (ticket == generation) finish(null) }.also { handler.postDelayed(it, 12_000) }
        } catch (_: SecurityException) { finish("系统未允许局域网发现，请检查权限或手动填写地址") }
        catch (_: IllegalArgumentException) { finish("系统暂时无法开始发现，请重新寻找或手动填写地址") }
    }

    @RequiresApi(34)
    private fun resolveCurrent(key: String, service: NsdServiceInfo, ticket: Long) {
        val callback = object : NsdManager.ServiceInfoCallback {
            override fun onServiceInfoCallbackRegistrationFailed(errorCode: Int) = post(ticket) {
                closers.remove(key)
                mutableState.value = mutableState.value.copy(error = "部分服务信息未能读取，可重新寻找或手动填写地址")
            }
            override fun onServiceUpdated(info: NsdServiceInfo) = post(ticket) {
                if (services.containsKey(key)) {
                    services[key] = serverAdvertisement(info.attributes, info.hostAddresses.mapNotNull { it.hostAddress }, info.port)
                    publish()
                }
            }
            override fun onServiceLost() = post(ticket) { services.remove(key); publish() }
            override fun onServiceInfoCallbackUnregistered() = Unit
        }
        try {
            manager.registerServiceInfoCallback(service, executor, callback)
            closers[key] = { try { manager.unregisterServiceInfoCallback(callback) }
                catch (_: IllegalArgumentException) { /* Already unregistered by the system. */ }
                catch (_: SecurityException) { mutableState.value = mutableState.value.copy(error = "局域网权限已变化，请重新核对授权") } }
        } catch (_: IllegalArgumentException) { services.remove(key) }
        catch (_: SecurityException) { finish("系统未允许读取局域网服务信息，请检查权限") }
    }

    @Suppress("DEPRECATION")
    private fun resolveNextLegacy(ticket: Long) {
        if (legacyResolving || legacyPending.isEmpty() || ticket != generation) return
        val (key, service) = legacyPending.removeFirst()
        legacyResolving = true
        val callback = object : NsdManager.ResolveListener {
            override fun onResolveFailed(info: NsdServiceInfo, errorCode: Int) = post(ticket) {
                legacyResolving = false; resolveNextLegacy(ticket)
            }
            override fun onServiceResolved(info: NsdServiceInfo) = post(ticket) {
                if (services.containsKey(key)) {
                    services[key] = serverAdvertisement(info.attributes, listOfNotNull(info.host?.hostAddress), info.port)
                    publish()
                }
                legacyResolving = false; resolveNextLegacy(ticket)
            }
        }
        try { manager.resolveService(service, callback) }
        catch (_: IllegalArgumentException) { legacyResolving = false; resolveNextLegacy(ticket) }
        catch (_: SecurityException) { finish("系统未允许读取局域网服务信息，请检查权限") }
    }

    private fun post(ticket: Long, action: () -> Unit) { handler.post { if (ticket == generation) action() } }
    private fun publish() {
        mutableState.value = mutableState.value.copy(candidates = services.values.flatten().distinctBy { it.address }.take(32))
    }
    private fun finish(error: String?) {
        stopInternal()
        mutableState.value = mutableState.value.copy(finished = true, error = error ?: mutableState.value.error)
    }
    private fun stopInternal() {
        generation++
        probeJob?.cancel(); probeJob = null
        expiry?.let(handler::removeCallbacks); expiry = null
        listener?.let { try { manager.stopServiceDiscovery(it) }
            catch (_: IllegalArgumentException) { /* Start failed or already stopped. */ }
            catch (_: SecurityException) { mutableState.value = mutableState.value.copy(error = "局域网权限已变化，请重新核对授权") } }
        listener = null
        closers.values.toList().forEach { it() }; closers.clear()
        legacyPending.clear(); legacyResolving = false
        multicast?.let { if (it.isHeld) it.release() }; multicast = null
        mutableState.value = mutableState.value.copy(searching = false)
    }
}
