package media.qimeng.next.data

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.util.Log
import coil3.ComponentRegistry
import coil3.decode.DataSource
import coil3.decode.ImageSource
import coil3.fetch.Fetcher
import coil3.fetch.SourceFetchResult
import coil3.key.Keyer
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.sync.withPermit
import kotlinx.coroutines.withContext
import media.qimeng.next.model.Asset
import media.qimeng.next.model.Session
import media.qimeng.next.model.ThumbnailCacheSelection
import media.qimeng.next.model.ThumbnailCoverage
import media.qimeng.next.model.ThumbnailPool
import media.qimeng.next.model.ThumbnailPoolUsage
import media.qimeng.next.model.ThumbnailPrefetch
import media.qimeng.next.model.ThumbnailPrefetchStatus
import media.qimeng.next.model.ThumbnailUsage
import media.qimeng.next.model.sameTarget
import okhttp3.Call
import okhttp3.Callback
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Request
import okhttp3.Response
import okio.Buffer
import java.io.File
import java.io.IOException
import java.net.URI
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger

private const val PREFETCH_CONCURRENCY = 4

/**
 * 前台刷新让路窗口（秒级粒度）：刷新首屏与全库预取在局域网带宽上互抢是旧版实测过的
 * 刷新慢叠加因素，旧版以 15s 有界窗口 + 1s 复查让路。窗口自愈过期，不需要恢复调用。
 */
private const val PREFETCH_YIELD_WINDOW_MS = 15_000L
private const val PREFETCH_YIELD_POLL_MS = 1_000L

/**
 * Single source for the local preview namespace. [VERSION] must mirror the server's
 * `PreviewPolicyVersion`; [SIZE] is the only size this client requests.
 */
internal object ThumbnailCachePolicy {
    /**
     * 目录命名空间。v3 起磁盘键只含实例身份（同一实例换地址不重下）；v2 的键编入了地址，
     * 升级后永远命中不到，由 [ThumbnailCacheRepository] 首次运行时整目录回收。
     */
    const val DIRECTORY = "thumbnail-previews-v3"
    /** 被取代的旧命名空间（键含地址），只用于升级时一次性回收。 */
    const val SUPERSEDED_DIRECTORY = "thumbnail-previews-v2"
    const val VERSION = "v5"
    const val SIZE = "md"

    /** Identity hashed into each disk key, so policy or size changes cannot reuse old bytes. */
    fun assetKey(assetId: String): String = "$VERSION:$SIZE:$assetId"
}

class ThumbnailCacheRepository internal constructor(
    context: Context,
    private val settings: SecureSettings,
    private val provider: ApiProvider,
    private val unmetered: Flow<Boolean> = unmeteredNetwork(context),
    directory: File = File(context.cacheDir, ThumbnailCachePolicy.DIRECTORY),
    private val revisions: ThumbnailRevisionRepository = ThumbnailRevisionRepository(provider),
) {
    private val selections = context.getSharedPreferences("thumbnail-instances", Context.MODE_PRIVATE)
    /** 缓存根目录，用于升级时定位被取代的命名空间（构造函数参数本身只在初始化期可见）。 */
    private val cacheRoot = directory
    /** 旧命名空间只回收一次；所有调用点都在 IO 线程，删除大目录不阻塞界面。 */
    private val supersededCleanup = AtomicBoolean(false)
    private val nas = ThumbnailDiskCache(File(directory, "nas"), 1024L * 1024 * 1024)
    private val local = ThumbnailDiskCache(File(directory, "local"), 512L * 1024 * 1024)
    /** 每个物理缓存池一条修订记录：清空一个池只作废该池的 SKIP 证据，另一个池照旧跳过全量拉取。 */
    private val revisionStores = ThumbnailPool.entries.associateWith {
        ThumbnailPrefetchRevisionStore(context, it.name.lowercase())
    }
    private val clearSignal = MutableStateFlow(0L)
    private val prefetchLock = Mutex()
    private val backgroundFetch = Semaphore(PREFETCH_CONCURRENCY)
    private val flights = ThumbnailFlights()
    private var capturedConnection: Connection? = null
    @Volatile private var clearedConnection: Connection? = null
    private val prefetchState = MutableStateFlow<ThumbnailPrefetch?>(null)
    private val nasDiskChanges = MutableStateFlow(0L)
    private val localDiskChanges = MutableStateFlow(0L)
    /** 前台刷新让路截止时间（EpochMs，0=无）。背景预取在此窗口内不发起新下载。 */
    @Volatile private var foregroundUntilMs = 0L
    val prefetch = prefetchState.asStateFlow()

    /** Per-pool counters so writes to one pool never force a rescan of the other pool's card. */
    val nasDiskRevision = nasDiskChanges.asStateFlow()
    val localDiskRevision = localDiskChanges.asStateFlow()

    private data class Connection(val target: Session, val revision: Long, val selection: ThumbnailCacheSelection)
    private data class Round(val connection: Connection, val allowed: Boolean, val cleared: Boolean)
    private data class Preview(val assetId: String, val version: Long, val connection: Connection, val generation: Long) {
        // The policy and size live inside the hashed key so early Go previews and future policy
        // revisions can never collide with this cache generation.
        val key = "${connection.selection.instanceKey}:${thumbnailHash(ThumbnailCachePolicy.assetKey(assetId))}"
        val flightKey = "$key:${connection.revision}:$generation"
    }

    private fun diskFor(pool: ThumbnailPool) = if (pool == ThumbnailPool.LOCAL) local else nas

    private fun noteDiskChange(pool: ThumbnailPool) {
        if (pool == ThumbnailPool.LOCAL) localDiskChanges.update { it + 1 } else nasDiskChanges.update { it + 1 }
    }

    /** Stable server identity matching the cache namespace: embedded restarts keep their key. */
    private fun serverKey(connection: Connection): String =
        "${connection.selection.pool.name}:${connection.selection.instanceKey}"

    private fun currentConnection(): Connection? = synchronized(settings) {
        val target = settings.session.value ?: return null
        val revision = settings.transportRevision.value
        capturedConnection?.takeIf { it.revision == revision && it.target == target }?.let { return it }
        // 内嵌本机服务现在使用回环 HTTP，没有证书可作标记；回环也可能是用户为 NAS 配置的隧道，
        // 所以以登录入口为准。升级前的会话没有该记录时，仍按证书或回环地址识别，避免旧缓存换池。
        val embedded = when (settings.loginEndpointFor(target)) {
            "local" -> true
            "nas" -> false
            else -> settings.certificateFor(target) != null || isLoopback(target.baseUrl)
        }
        val kind = if (embedded) ThumbnailPool.LOCAL else ThumbnailPool.NAS
        // 磁盘命名空间只由服务器实例身份决定：同一实例可以经 LAN 地址、隧道域名或回环访问，
        // 换地址不等于换库。旧版 SignedMediaCacheKeys 剥掉 scheme/host 就是同一裁决；把 baseUrl
        // 编进缓存键会让一次地址切换把整库缩略图判为未缓存、全部重下。
        // 传输修订（transportRevision）仍留在 Preview.flightKey 里——那是内存/在途键：请求绑定
        // 具体连接，连接变了旧请求不能算数；磁盘条目按 (实例, 资产, 内容版本) 复用，与连接无关。
        val identity = target.instanceId
        Connection(target, revision, ThumbnailCacheSelection(kind, thumbnailHash(identity))).also { capturedConnection = it }
    }

    private fun isLoopback(baseUrl: String): Boolean = try {
        URI(baseUrl).host?.lowercase() in setOf("127.0.0.1", "localhost", "::1", "[::1]")
    } catch (_: Exception) { false }

    private fun preview(asset: Asset): Preview? {        if (asset.thumbnailUrl.isBlank()) return null
        val connection = currentConnection()?.takeIf { sameTarget(asset.target, it.target) } ?: return null
        return Preview(asset.id, asset.version, connection, diskFor(connection.selection.pool).generation)
    }

    fun addImageComponents(builder: ComponentRegistry.Builder) {
        builder.add(Keyer<Asset> { asset, _ ->
            preview(asset)?.let { "${it.flightKey}:${it.version}" }
        })
        builder.add(Fetcher.Factory<Asset> { asset, options, _ ->
            val request = preview(asset) ?: return@Factory null
            Fetcher {
                // 磁盘命中直接交出文件读出的 Buffer，网络命中交出已下载的 Buffer：
                // 两者都不再经 ByteArray 中转，Coil 从同一份缓冲解码。
                val (bytes, source) = withContext(Dispatchers.IO) { fetchVisible(request) }
                SourceFetchResult(ImageSource(bytes, options.fileSystem), "image/jpeg", source)
            }
        })
    }

    private suspend fun fetchVisible(preview: Preview): Pair<Buffer, DataSource> =
        flights.run(preview.flightKey) {
            requireCurrent(preview.connection)
            val disk = diskFor(preview.connection.selection.pool)
            val stored = disk.read(preview.key, preview.version)
            if (stored != null) {
                requireCurrent(preview.connection)
                return@run stored to DataSource.DISK
            }
            val payload = download(preview, background = false)
            currentCoroutineContext().ensureActive()
            requireCurrent(preview.connection)
            if (payload.verified) {
                val result = disk.store(preview.key, preview.version, payload.bytes, preview.generation, background = false) {
                    currentConnection() == preview.connection
                }
                if (result == ThumbnailDiskResult.READY) noteDiskChange(preview.connection.selection.pool)
            }
            requireCurrent(preview.connection)
            payload.bytes to DataSource.NETWORK
        }

    private fun requireCurrent(connection: Connection) {
        if (currentConnection() != connection) throw CancellationException("缩略图所属连接已改变")
    }

    /** 前台刷新开始时让路：背景预取在下个复查周期暂停下载，窗口到期自愈（幂等，可重复续期）。 */
    fun pauseForForegroundRefresh() {
        foregroundUntilMs = System.currentTimeMillis() + PREFETCH_YIELD_WINDOW_MS
    }

    private suspend fun yieldForForegroundRefresh() {
        while (System.currentTimeMillis() < foregroundUntilMs) delay(PREFETCH_YIELD_POLL_MS)
    }

    /**
     * 视口前瞻：把字节写进磁盘池，返回这些字节是否已可读。
     * 返回 true 时调用方可以再用同一个 ImageLoader 解码一次，把位图预热进内存缓存
     * （旧版网格预取的 `imageLoader.execute` 语义）；false 表示没有落盘，不必解码。
     *
     * 调用方是视口队列的长驻 worker，只在离开页面时取消；这里不做任何逐窗口取消。
     */
    suspend fun prefetchNearby(assets: List<Asset>): Boolean = withContext(Dispatchers.IO) {
        val connection = currentConnection() ?: return@withContext false
        // 视口前瞻不做计费网络门（旧版网格预取同样不设门）：这些条目是用户正在滚入、
        // 无论如何都会请求的缩略图，提前 1 个往返不增加总流量；全库预取仍按未计费网络门控。
        if (clearedConnection == connection) return@withContext false
        val pool = connection.selection.pool
        val disk = diskFor(pool)
        val generation = disk.generation
        var warmed = true
        for (asset in assets) {
            requireCurrent(connection)
            if (asset.thumbnailUrl.isBlank() || asset.mediaType == "animated_image" || !sameTarget(asset.target, connection.target)) {
                warmed = false
                continue
            }
            val request = Preview(asset.id, asset.version, connection, generation)
            try {
                when (prefetchOne(request, disk, lookahead = true)) {
                    ThumbnailDiskResult.READY -> Unit
                    ThumbnailDiskResult.FULL -> return@withContext false
                    ThumbnailDiskResult.MISSING -> warmed = false
                }
            } catch (failure: ApiFailure) {
                if (failure.isMissingThumbnail()) {
                    warmed = false
                    continue
                }
                Log.d("ThumbnailCache", "Nearby thumbnail prefetch stopped (HTTP ${failure.status})")
                return@withContext false
            } catch (_: IOException) {
                Log.d("ThumbnailCache", "Nearby thumbnail prefetch stopped after an I/O error")
                return@withContext false
            }
        }
        warmed
    }

    private fun selected(pool: ThumbnailPool, current: Connection?): ThumbnailCacheSelection? {
        if (current?.selection?.pool == pool) return current.selection
        return selections.getString(pool.name, null)?.let { ThumbnailCacheSelection(pool, it) }
    }

    suspend fun usage(pool: ThumbnailPool): ThumbnailPoolUsage = withContext(Dispatchers.IO) {
        removeSupersededNamespace()
        val selection = selected(pool, currentConnection())
        ThumbnailPoolUsage(selection, selection?.let { diskFor(pool).usage(it.instanceKey) } ?: ThumbnailUsage(0, 0))
    }

    /**
     * 升级一次性回收：v2 目录的键包含服务器地址，本版只按实例寻址，旧条目永远不会再被读取；
     * 留着的目录仍占最多 1 GiB（计入旧池的 LRU 预算），而 [usage] 只扫描新目录，卡片会报 0。
     * 删除被取代的命名空间后显示与实际磁盘一致，首次预取也不必重下已失效的旧字节。
     * 目录不存在时 deleteRecursively 无副作用；删除失败只记日志，下次进程启动会再试。
     * 只在 IO 线程（预取循环/缓存统计）调用，避免在构造或组合线程上做多秒磁盘遍历。
     */
    private fun removeSupersededNamespace() {
        if (!supersededCleanup.compareAndSet(false, true)) return
        val parent = cacheRoot.parentFile ?: return
        val superseded = File(parent, ThumbnailCachePolicy.SUPERSEDED_DIRECTORY)
        if (superseded.exists() && !superseded.deleteRecursively())
            Log.w("ThumbnailCache", "Superseded thumbnail cache could not be removed")
    }

    suspend fun clear(selection: ThumbnailCacheSelection) = withContext(Dispatchers.IO) {
        val current = currentConnection()
        if (selected(selection.pool, current) != selection) throw IOException("连接已改变，请重新确认清空范围")
        if (current?.selection == selection) {
            clearedConnection = current
            clearSignal.update { it + 1 }
        }
        try {
            try {
                diskFor(selection.pool).clear(selection.instanceKey)
            } finally {
                // A retained revision record would make the next round skip an empty pool; the
                // record is scoped per pool so the other pool keeps its skip evidence.
                revisionStores.getValue(selection.pool).clear()
            }
        } finally {
            noteDiskChange(selection.pool)
        }
    }

    suspend fun localCoverage(): Pair<ThumbnailCacheSelection, ThumbnailCoverage>? = withContext(Dispatchers.IO) {
        val connection = currentConnection()?.takeIf { it.selection.pool == ThumbnailPool.LOCAL } ?: return@withContext null
        val progress = provider.api(connection.target).getThumbnailProgress().bodyOrThrow()
        verifyInstance(connection, progress.instanceId)
        requireCurrent(connection)
        connection.selection to ThumbnailCoverage(progress.thumbsOnDisk, progress.totalAssets)
    }

    /**
     * The one application-scoped prefetch loop. [DataGraph] starts it once from
     * [QimengApplication]; it is not tied to any activity's STARTED state.
     *
     * The loop re-arms on transport changes (login, logout, server switch) and on cache clears.
     * `collectLatest` cancels the previous round's listing and in-flight downloads when the
     * connection changes, so logout and switching servers still stop the old work.
     */
    suspend fun runPrefetchLoop() = prefetchLock.withLock {
        withContext(Dispatchers.IO) {
            // 首次运行先回收旧命名空间（应用启动即进入本循环），再开始预取。
            removeSupersededNamespace()
            combine(settings.transportRevision, unmetered, clearSignal) { _, networkAllowed, _ ->
                currentConnection()?.let {
                    Round(it, networkAllowed || it.selection.pool == ThumbnailPool.LOCAL, clearedConnection == it)
                }
            }.distinctUntilChanged().collectLatest { round ->
                prefetchState.value = round?.let { ThumbnailPrefetch(it.connection.selection, ThumbnailPrefetchStatus.Idle) }
                if (round == null) return@collectLatest
                val (connection, allowed, cleared) = round
                val selection = connection.selection
                if (selections.getString(selection.pool.name, null) != selection.instanceKey) {
                    selections.edit().putString(selection.pool.name, selection.instanceKey).apply()
                }
                if (cleared) return@collectLatest
                if (!allowed) {
                    publish(connection, ThumbnailPrefetchStatus.WaitingForNetwork)
                    return@collectLatest
                }
                delay(1_000)
                while (currentConnection() == connection && clearedConnection != connection) {
                    try {
                        fetchRound(connection)
                        delay(10 * 60_000L)
                    } catch (cancelled: CancellationException) {
                        throw cancelled
                    } catch (failure: Exception) {
                        publish(connection, ThumbnailPrefetchStatus.Failed(
                            if (failure is ApiFailure) failure.message else "缩略图预取暂未完成，稍后会重试"
                        ))
                        delay(60_000)
                    }
                }
            }
        }
    }

    private suspend fun fetchRound(connection: Connection) {
        val pool = connection.selection.pool
        val disk = diskFor(pool)
        val generation = disk.generation
        val revision = readRevision(connection)
        val serverKey = serverKey(connection)
        if (revision != null && ThumbnailPrefetchGate.decide(
                revision.revision, readLastDone(pool), serverKey, revision.thumbnailVersion,
            ) == ThumbnailPrefetchDecision.SKIP
        ) {
            val sample = readStoredSample(pool)
            val missing = sample.count { disk.warmState(it.key, it.version) != ThumbnailDiskResult.READY }
            if (!ThumbnailPrefetchSampleGate.shouldDowngradeToFull(sample.size, missing)) {
                publish(connection, ThumbnailPrefetchStatus.Skipped)
                return
            }
            Log.d("ThumbnailCache", "Revision sample missing $missing/${sample.size}; running a full prefetch round")
        }
        val api = provider.api(connection.target)
        var cursor: String? = null
        var lastId: String? = null
        var seen = 0L
        var total = 0L
        var cached = 0L
        var atCapacity = false
        val progressLock = Mutex()
        val samples = mutableListOf<ThumbnailPrefetchSample>()
        do {
            requireCurrent(connection)
            val page = api.listThumbnailCandidates(cursor = cursor, limit = 100).bodyOrThrow()
            verifyInstance(connection, page.instanceId)
            if (cursor == null) {
                total = page.totalMatched ?: throw IOException("预取清单缺少候选数")
                publish(connection, ThumbnailPrefetchStatus.Running(cached, total))
            }
            if (page.items.size > 100 || (page.nextCursor != null && (page.nextCursor == cursor || page.items.isEmpty()))) {
                throw IOException("预取清单游标无效")
            }
            val previews = page.items.map { candidate ->
                val nextId = candidate.assetId
                val previousId = lastId
                if (nextId.isEmpty() || (previousId != null && nextId <= previousId) || candidate.contentVersion < 1) {
                    throw IOException("预取清单版本或顺序无效")
                }
                lastId = nextId
                Preview(nextId, candidate.contentVersion, connection, generation)
            }
            val next = AtomicInteger(0)
            coroutineScope {
                repeat(minOf(PREFETCH_CONCURRENCY, previews.size)) {
                    launch {
                        while (true) {
                            currentCoroutineContext().ensureActive()
                            val index = next.getAndIncrement()
                            if (index >= previews.size) break
                            // 前台刷新在途时逐周期让路（旧版 ThumbnailPrefetchThrottle 同款语义）。
                            yieldForForegroundRefresh()
                            val preview = previews[index]
                            val result = try {
                                prefetchOne(preview, disk)
                            } catch (failure: ApiFailure) {
                                if (!failure.isMissingThumbnail()) throw failure
                                ThumbnailDiskResult.MISSING
                            }
                            progressLock.withLock {
                                when (result) {
                                    ThumbnailDiskResult.READY -> {
                                        cached++
                                        samples += ThumbnailPrefetchSample(preview.key, preview.version)
                                    }
                                    ThumbnailDiskResult.FULL -> atCapacity = true
                                    ThumbnailDiskResult.MISSING -> Unit
                                }
                                seen++
                                total = maxOf(total, seen)
                                publish(connection, ThumbnailPrefetchStatus.Running(cached, total))
                            }
                        }
                    }
                }
            }
            cursor = page.nextCursor
        } while (cursor != null)
        recordRound(pool, revision, serverKey, samples)
        publish(connection, ThumbnailPrefetchStatus.Done(cached, total, atCapacity))
    }

    private suspend fun readRevision(connection: Connection): ThumbnailRevisionSnapshot? = try {
        revisions.fetch(connection.target)
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (_: Exception) {
        null
    }

    private fun readLastDone(pool: ThumbnailPool): ThumbnailPrefetchRecord? = try {
        revisionStores.getValue(pool).lastDone()
    } catch (_: Exception) {
        null
    }

    private fun readStoredSample(pool: ThumbnailPool): Set<ThumbnailPrefetchSample> = try {
        revisionStores.getValue(pool).storedSample()
    } catch (_: Exception) {
        emptySet()
    }

    /**
     * Writes the revision read at the start of this round together with a random sample of the
     * entries verified during it. A null revision (old server or parse failure) keeps the old
     * record, so the next round runs full again rather than skipping on stale evidence.
     */
    private fun recordRound(
        pool: ThumbnailPool,
        revision: ThumbnailRevisionSnapshot?,
        serverKey: String,
        samples: List<ThumbnailPrefetchSample>,
    ) {
        if (revision == null) return
        try {
            revisionStores.getValue(pool).setLastDone(
                ThumbnailPrefetchRecord(serverKey, revision.revision, revision.thumbnailVersion),
                ThumbnailPrefetchSampleGate.pickSample(samples),
            )
        } catch (_: Exception) {
            Log.w("ThumbnailCache", "Prefetch revision record could not be saved; the next round will be full")
        }
    }

    /**
     * 预取一条预览。
     *
     * 预取**不再占用可见请求的 flight**：同一 key 的可见加载会自己下载，而不是排在一条
     * 可能很慢的预取下载后面（旧版网格预取与可见图各自入队，谁先到谁先出）。重复下载
     * 只发生在「预热中的格子恰好滚入视口」这一窗口，且磁盘写入由 [ThumbnailDiskCache.store]
     * 串行化，结果一致。lookahead=true 的视口预取另外跳过后台配额：它服务的是马上要看的
     * 格子，不能排在 4 条全库预取后面。
     */
    private suspend fun prefetchOne(preview: Preview, disk: ThumbnailDiskCache, lookahead: Boolean = false): ThumbnailDiskResult {
        val current = disk.warmState(preview.key, preview.version)
        if (current != ThumbnailDiskResult.MISSING) return current
        if (lookahead) return downloadMissing(preview, disk, lookahead = true)
        return backgroundFetch.withPermit { downloadMissing(preview, disk, lookahead = false) }
    }

    private suspend fun downloadMissing(preview: Preview, disk: ThumbnailDiskCache, lookahead: Boolean): ThumbnailDiskResult {
        requireCurrent(preview.connection)
        if (disk.generation != preview.generation || clearedConnection == preview.connection) {
            throw CancellationException("缩略图缓存已清空")
        }
        val existing = disk.warmState(preview.key, preview.version)
        if (existing != ThumbnailDiskResult.MISSING) return existing
        val payload = download(preview, background = true, lookahead)
        currentCoroutineContext().ensureActive()
        requireCurrent(preview.connection)
        val result = disk.store(preview.key, preview.version, payload.bytes, preview.generation, background = true) {
            currentConnection() == preview.connection && clearedConnection != preview.connection
        }
        if (result == ThumbnailDiskResult.READY) noteDiskChange(preview.connection.selection.pool)
        return result
    }

    private fun publish(connection: Connection, status: ThumbnailPrefetchStatus) {
        if (currentConnection() == connection && clearedConnection != connection) {
            prefetchState.value = ThumbnailPrefetch(connection.selection, status)
        }
    }

    private fun verifyInstance(connection: Connection, instanceId: String?) {
        if (instanceId != connection.target.instanceId) throw ApiFailure(409, "地址已指向另一个媒体库，请重新登录", "instance_changed")
    }

    /** 下载结果直接持有 okio 缓冲：正文只经「网络 → Buffer」一次拷贝，落盘用 clone 共享片段。 */
    private data class Payload(val bytes: Buffer, val verified: Boolean)

    private suspend fun download(preview: Preview, background: Boolean, lookahead: Boolean = false): Payload {
        val target = preview.connection.target
        val url = target.baseUrl.toHttpUrl().newBuilder()
            .addPathSegments("api/v1/assets").addPathSegment(preview.assetId).addPathSegment("thumbnail")
            .addQueryParameter("version", preview.version.toString()).addQueryParameter("size", ThumbnailCachePolicy.SIZE)
            .apply { if (background) addQueryParameter("background", "true") }.build()
        // 可见项、视口前瞻与后台全库预取分属不同 Dispatcher：任一方在途都不会占掉另一方的
        // 每主机并发名额（旧版 Coil 客户端与列表 SDK 客户端也是分开的）。
        val client = when {
            lookahead -> provider.lookaheadMediaClient(target)
            background -> provider.backgroundMediaClient(target)
            else -> provider.mediaClient(target)
        }
        val call = client.newCall(Request.Builder().url(url).header("Accept", "image/jpeg").build())
        call.timeout().timeout(65, TimeUnit.SECONDS)
        return suspendCancellableCoroutine { continuation ->
            continuation.invokeOnCancellation { call.cancel() }
            call.enqueue(object : Callback {
                override fun onFailure(call: Call, e: IOException) {
                    continuation.resumeWith(Result.failure(e))
                }

                override fun onResponse(call: Call, response: Response) {
                    continuation.resumeWith(runCatching {
                        response.use {
                            if (it.code != 200) throw ApiFailure(it.code, "缩略图请求失败（HTTP ${it.code}）")
                            val mediaInstance = it.header("X-Qimeng-Instance-Id")
                            if (background || mediaInstance != null) verifyInstance(preview.connection, mediaInstance)
                            val body = it.body ?: throw IOException("缩略图正文缺失")
                            if (body.contentType()?.toString()?.substringBefore(';') != "image/jpeg") {
                                throw IOException("缩略图格式无效")
                            }
                            // 直接读进 Buffer 交给解码器；落盘时 store 用 clone() 共享片段，
                            // 不再有 ByteArray 中转或第二份整块缓冲。
                            val buffer = Buffer()
                            val source = body.source()
                            while (true) {
                                val read = source.read(buffer, minOf(8192L, MAX_THUMBNAIL_BYTES + 1 - buffer.size))
                                if (read == -1L) break
                                if (buffer.size > MAX_THUMBNAIL_BYTES) throw IOException("缩略图响应超出读取预算")
                            }
                            if (!validJpeg(buffer)) throw IOException("缩略图正文不是完整 JPEG")
                            Payload(buffer, mediaInstance != null)
                        }
                    })
                }
            })
        }
    }

    internal fun closeForTest() {
        nas.shutdown()
        local.shutdown()
    }
}

private fun ApiFailure.isMissingThumbnail(): Boolean =
    status in setOf(404, 409, 415, 422, 501) && code != "instance_changed"

private class ThumbnailFlights {
    private data class Flight(val mutex: Mutex = Mutex(), var users: Int = 0)
    private val active = mutableMapOf<String, Flight>()

    suspend fun <T> run(key: String, read: suspend () -> T): T {
        val flight = synchronized(active) { active.getOrPut(key) { Flight() }.also { it.users++ } }
        try {
            return flight.mutex.withLock { read() }
        } finally {
            synchronized(active) {
                flight.users--
                if (flight.users == 0) active.remove(key)
            }
        }
    }
}

private fun unmeteredNetwork(context: Context): Flow<Boolean> = callbackFlow {
    val manager = context.getSystemService(ConnectivityManager::class.java)
    if (manager == null) {
        trySend(false)
        close()
        return@callbackFlow
    }
    var current = manager.activeNetwork
    trySend(current?.let(manager::getNetworkCapabilities)?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED) == true)
    val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) {
            current = network
            trySend(false)
        }

        override fun onLost(network: Network) {
            if (network == current) {
                current = null
                trySend(false)
            }
        }

        override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) {
            if (network == current) trySend(capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED))
        }
    }
    manager.registerDefaultNetworkCallback(callback)
    awaitClose { manager.unregisterNetworkCallback(callback) }
}
