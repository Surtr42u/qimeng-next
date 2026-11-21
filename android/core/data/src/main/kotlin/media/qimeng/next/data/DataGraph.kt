package media.qimeng.next.data

import android.content.Context
import kotlinx.coroutines.*
import media.qimeng.next.data.events.ActivityVisibility
import media.qimeng.next.data.events.ServerEventConsumer
import media.qimeng.next.model.Asset
import media.qimeng.next.model.PendingEvent
import okhttp3.sse.EventSources
import java.time.Instant
import java.util.UUID

/** Application-scoped composition root shared by foreground UI and WorkManager. */
class DataGraph private constructor(context: Context) {
    val settings = SecureSettings(context)
    internal val provider = ApiProvider(settings)
    /** One live invalidation stream, active only while a session exists and a screen is visible. */
    val serverEvents = ServerEventConsumer(settings.session, factoryFor = { session ->
        EventSources.createFactory(provider.client(session))
    })
    private val activityVisibility = ActivityVisibility(context) { visible -> serverEvents.setForeground(visible) }
    val queue = QueueStore(context)
    val media = MediaRepository(settings, provider)
    /**
     * 首帧只构造首屏真正会读到的对象（设置/凭据、API 客户端、传输队列、媒体查询、上传调度）。
     * 其余仓储一律按需构造：旧版这些等价物是 Hilt 单例（进程内懒建），而本类若在构造期全部
     * 实例化，冷启动就要在主线程上把 8 个 SharedPreferences 文件、Gson 解析、SQLite 句柄和
     * 缩略图磁盘缓存全部打开一遍——首屏一个都不读。
     */
    val thumbnails by lazy { ThumbnailCacheRepository(context, settings, provider) }
    val analytics by lazy { AnalyticsRepository(settings, provider) }
    val management by lazy { ManagementRepository(settings, provider) }
    val extractionVocabulary by lazy { ExtractionVocabularyRepository(provider) }
    val aiAccess by lazy { AIAccessRepository(settings, provider) }
    val authorProfiles by lazy { AuthorProfileRepository(context, settings, provider) }
    val naming by lazy { NamingRepository(settings, provider) }
    val discovery by lazy { ServerDiscovery(context, media::discoverRemembered) }
    val scans by lazy { ScanRepository(settings, provider) }
    val archives by lazy { ArchiveRepository(context, provider, settings) }
    val backups by lazy { MaintenanceBackupRepository(context, provider, settings, archives).also { archives.retainedByMaintenance = it::referenced } }
    val authorLists by lazy { AuthorListRepository(context, provider) }
    val deviceDelivery by lazy { DeviceDeliveryRepository(context, settings, provider, backups) }
    internal val archiveProtection by lazy { ArchiveProtection(context, archives.store, provider, queue) }
    val uploads = UploadRepository(context, queue, settings)
    val activityEvents by lazy { ActivityEventQueue(queue, settings, uploads::schedule) }
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var thumbnailPrefetchJob: Job? = null

    /**
     * 进程启动恢复（队列日志、未完成上传调度、归档与备份续跑）。旧版由 Hilt 的
     * EventSyncBootstrapper 等在 onCreate 触发；本类改为首帧之后由壳显式触发
     * （[startRecovery]），恢复期的磁盘/网络 IO 不再和首屏首批请求抢同一份带宽与 IO。
     * LAZY 起步：构造 DataGraph 时不产生任何 IO，[startRecovery] 与 [awaitRecovery] 幂等地起跑。
     */
    private val recovery = scope.async(start = CoroutineStart.LAZY) {
            try { queue.load(); uploads.recoverInterruptedIntake(); uploads.schedule(); settings.session.value?.let { archives.resume(it); backups.resume(it) } }
            catch (failure: CancellationException) { throw failure }
            catch (failure: Exception) { queue.reportProblem(safeFailure(failure)) }
    }

    /** 起跑进程级恢复（幂等）。壳在首帧之后调用。 */
    fun startRecovery() { recovery.start() }

    /**
     * 预热缩略图仓储：首屏组合期 AppRoot 会同步调用 [ThumbnailCacheRepository.addImageComponents]
     * （`graph.thumbnails`），而它的构造要打开两个 Coil DiskCache 的 journal。这里在 IO 上提前
     * 建好实例，避免这次首次打开落在首帧的主线程上；lazy 保证只建一次，重复调用无害。
     */
    fun warmThumbnailCache() { scope.launch { thumbnails } }

    suspend fun awaitRecovery() { recovery.start(); recovery.await(); if (queue.state.value == null) queue.load() }

    /**
     * Starts the single application-scoped thumbnail prefetch loop. Callers are idempotent:
     * a second invocation reuses the active job instead of opening another loop.
     */
    fun startThumbnailPrefetch() {
        synchronized(this) {
            if (thumbnailPrefetchJob?.isActive == true) return
            thumbnailPrefetchJob = scope.launch {
                try {
                    thumbnails.runPrefetchLoop()
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (failure: Exception) {
                    queue.reportProblem(safeFailure(failure))
                }
            }
        }
    }

    /** A normal activity exit must not cancel the final visible segment's journal commit. */
    fun recordEvent(asset: Asset, kind: String, sessionId: String, positionMs: Long = 0, seconds: Double = 0.0,
                    occurredAt: String = Instant.now().toString()) {
        scope.launch {
            try {
                if (queue.state.value == null) queue.load()
                activityEvents.append(PendingEvent(UUID.randomUUID().toString(), asset.target, asset.id, kind,
                    positionMs.coerceAtLeast(0), asset.version, sessionId, seconds, occurredAt, assetName = asset.name.take(240)))
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { queue.reportProblem(safeFailure(failure)) }
        }
    }

    init {
        // Process-scoped: the stream follows the selected session and real
        // activity visibility, not a single screen's composition.
        activityVisibility.register()
        serverEvents.start()
    }

    companion object {
        @Volatile private var instance: DataGraph? = null
        fun get(context: Context): DataGraph = instance ?: synchronized(this) {
            instance ?: DataGraph(context.applicationContext).also { instance = it }
        }
    }
}
