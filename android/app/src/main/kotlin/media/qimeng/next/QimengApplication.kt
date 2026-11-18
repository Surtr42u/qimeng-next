package media.qimeng.next

import android.app.Application
import media.qimeng.next.data.DataGraph
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.CancellationException
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.runtime.*

class QimengApplication : Application(), LocalScanHost, media.qimeng.next.data.DeviceDeliveryHost {
    val graph by lazy { DataGraph.get(this) }
    override val runtime by lazy { createRuntime(this) }
    private val runtimeScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    val localConnection by lazy { LocalConnectionCoordinator(graph, runtime, runtimeScope) }

    override fun isSelectedLocalInstance(target: media.qimeng.next.model.Session) = runtime.available && runtime.shouldRestore &&
        runtime.status.value.phase == RuntimePhase.READY && target.baseUrl in runtime.ownedEndpoints(target.instanceId) &&
        media.qimeng.next.model.sameTarget(target, graph.settings.session.value)
    override fun onCreate() {
        super.onCreate()
        if (runtime.available) localConnection
    }

    override suspend fun submitLocalScan(connection: LocalConnection, request: LocalScanRequest): String = try {
        graph.scans.startOwned(connection.baseUrl, connection.instanceId, connection.certificateDerBase64, connection.bootstrapToken,
            request.libraryId, request.expectedRevision, "local-scan-${request.id}")
    } catch (cancelled: CancellationException) { throw cancelled }
    catch (failure: ApiFailure) { throw LocalScanFailure(failure.message, failure.status == 429 || failure.status >= 500) }
    catch (_: java.io.IOException) { throw LocalScanFailure("连接中断，待扫任务保留；恢复后核对原提交结果", true) }
    catch (_: IllegalStateException) { throw LocalScanFailure("本机实例核验未通过，已停止提交待扫任务", false) }
}
