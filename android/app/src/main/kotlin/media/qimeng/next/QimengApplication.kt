package media.qimeng.next

import android.app.Application
import media.qimeng.next.data.DataGraph
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.CancellationException
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.runtime.*
import java.util.concurrent.atomic.AtomicBoolean

class QimengApplication : Application(), LocalScanHost, media.qimeng.next.data.DeviceDeliveryHost {
    val graph by lazy { DataGraph.get(this) }
    override val runtime by lazy { createRuntime(this) }
    private val runtimeScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    val localConnection by lazy { LocalConnectionCoordinator(graph, runtime, runtimeScope) }
    /** 首帧回调只认第一次：Activity 重建、多窗口都不重跑恢复/预取。 */
    private val firstFrame = AtomicBoolean(false)

    override fun isSelectedLocalInstance(target: media.qimeng.next.model.Session) = runtime.available && runtime.shouldRestore &&
        runtime.status.value.phase == RuntimePhase.READY && target.baseUrl in runtime.ownedEndpoints(target.instanceId) &&
        media.qimeng.next.model.sameTarget(target, graph.settings.session.value)

    /**
     * 冷启动不装配整个对象图。旧版把启动期工作挂在 Hilt 的单例上（构造即注入，等价物是
     * 进程内懒单例），本版此前在 onCreate 里连碰两处：
     *
     * 1) `graph.startThumbnailPrefetch()` 直接构造 DataGraph：SecureSettings 同步读
     *    SharedPreferences + Gson、QueueStore/UploadRepository 建目录、十几个仓储全量实例化，
     *    并把两个缩略图 DiskCache 的 journal 打开——全部发生在第一帧之前，而首屏一个都不读；
     * 2) 预取循环随即删旧目录、1 秒后开始全库拉取，与首页首批请求抢同一份 LAN 带宽和磁盘。
     *
     * 现在 onCreate 只留一次“有先发优势”的预热：缩略图磁盘缓存改在 IO 上先建（首帧组合期
     * AppRoot 会同步要它）；DataGraph 的其余仓储由首屏按需构造；进程级恢复与全库预取推迟到
     * 首帧提交之后（[onFirstFrame]）。事件流仍在 DataGraph 构造时连上（`serverEvents.start()`），
     * 行为不变。
     */
    override fun onCreate() {
        super.onCreate()
        // 这里只做“便宜且有先发优势”的一件：在 IO 上提前打开缩略图磁盘缓存，让首帧组合期
        // 注册 Coil 取图组件时命中已建好的实例（详见 DataGraph.warmThumbnailCache）。
        graph.warmThumbnailCache()
    }

    /**
     * 首帧已提交后调用一次（见 MainActivity 的 pre-draw + post）。这里只做“必须发生、但不必
     * 抢在第一帧之前”的进程级工作，顺序与代价都低于首屏首绘：
     *
     * 1) [DataGraph.startRecovery]：传输队列日志、未完成上传续跑、归档/备份恢复；
     * 2) [DataGraph.startThumbnailPrefetch]：全库缩略图预取（旧版由登录态观察驱动，同样
     *    不在首帧前起跑）。
     *
     * 两者都在 DataGraph 的 IO 作用域上执行，重复调用幂等。
     */
    fun onFirstFrame() {
        if (!firstFrame.compareAndSet(false, true)) return
        graph.startRecovery()
        graph.startThumbnailPrefetch()
    }

    override suspend fun submitLocalScan(connection: LocalConnection, request: LocalScanRequest): String = try {
        graph.scans.startOwned(connection.baseUrl, connection.instanceId, connection.certificateDerBase64, connection.bootstrapToken,
            request.libraryId, request.expectedRevision, "local-scan-${request.id}")
    } catch (cancelled: CancellationException) { throw cancelled }
    catch (failure: ApiFailure) { throw LocalScanFailure(failure.message, failure.status == 429 || failure.status >= 500) }
    catch (_: java.io.IOException) { throw LocalScanFailure("连接中断，待扫任务保留；恢复后核对原提交结果", true) }
    catch (_: IllegalStateException) { throw LocalScanFailure("本机实例核验未通过，已停止提交待扫任务", false) }
}
