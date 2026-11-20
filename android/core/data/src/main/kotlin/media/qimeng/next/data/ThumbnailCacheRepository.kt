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
import java.util.concurrent.atomic.AtomicInteger

private const val PREFETCH_CONCURRENCY = 4

/**
 * Single source for the local preview namespace. [VERSION] must mirror the server's
 * `PreviewPolicyVersion`; [SIZE] is the only size this client requests.
 */
internal object ThumbnailCachePolicy {
    const val DIRECTORY = "thumbnail-previews-v2"
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
    private val revisionStore: ThumbnailPrefetchRevisionStore = ThumbnailPrefetchRevisionStore(context),
) {
    private val selections = context.getSharedPreferences("thumbnail-instances", Context.MODE_PRIVATE)
    private val nas = ThumbnailDiskCache(File(directory, "nas"), 1024L * 1024 * 1024)
    private val local = ThumbnailDiskCache(File(directory, "local"), 512L * 1024 * 1024)
    private val clearSignal = MutableStateFlow(0L)
    private val prefetchLock = Mutex()
    private val backgroundFetch = Semaphore(PREFETCH_CONCURRENCY)
    private val flights = ThumbnailFlights()
    private var capturedConnection: Connection? = null
    @Volatile private var clearedConnection: Connection? = null
    private val prefetchState = MutableStateFlow<ThumbnailPrefetch?>(null)
    private val diskChanges = MutableStateFlow(0L)
    val prefetch = prefetchState.asStateFlow()
    val diskRevision = diskChanges.asStateFlow()

    private data class Connection(val target: Session, val revision: Long, val selection: ThumbnailCacheSelection)
    private data class Round(val connection: Connection, val allowed: Boolean, val cleared: Boolean)
    private data class Preview(val assetId: String, val version: Long, val connection: Connection, val generation: Long) {
        // The policy and size live inside the hashed key so early Go previews and future policy
        // revisions can never collide with this cache generation.
        val key = "${connection.selection.instanceKey}:${thumbnailHash(ThumbnailCachePolicy.assetKey(assetId))}"
        val flightKey = "$key:${connection.revision}:$generation"
    }

    private fun diskFor(pool: ThumbnailPool) = if (pool == ThumbnailPool.LOCAL) local else nas

    /** Stable server identity matching the cache namespace: embedded restarts keep their key. */
    private fun serverKey(connection: Connection): String =
        "${connection.selection.pool.name}:${connection.selection.instanceKey}"

    private fun currentConnection(): Connection? = synchronized(settings) {
        val target = settings.session.value ?: return null
        val revision = settings.transportRevision.value
        capturedConnection?.takeIf { it.revision == revision && it.target == target }?.let { return it }
        // 内嵌本机服务现在使用回环 HTTP，没有证书可作标记；回环地址仍归入独立的 local 池，
        // 并沿用稳定的 instanceId 命名空间，升级前后缓存键保持一致。
        val embedded = settings.certificateFor(target) != null || isLoopback(target.baseUrl)
        val kind = if (embedded) ThumbnailPool.LOCAL else ThumbnailPool.NAS
        val identity = if (embedded) target.instanceId else "${target.baseUrl}\n${target.instanceId}"
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
                val (bytes, source) = withContext(Dispatchers.IO) { fetchVisible(request) }
                SourceFetchResult(ImageSource(Buffer().write(bytes), options.fileSystem), "image/jpeg", source)
            }
        })
    }

    private suspend fun fetchVisible(preview: Preview): Pair<ByteArray, DataSource> =
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
                if (result == ThumbnailDiskResult.READY) diskChanges.update { it + 1 }
            }
            requireCurrent(preview.connection)
            payload.bytes to DataSource.NETWORK
        }

    private fun requireCurrent(connection: Connection) {
        if (currentConnection() != connection) throw CancellationException("缩略图所属连接已改变")
    }

    suspend fun prefetchNearby(assets: List<Asset>) = withContext(Dispatchers.IO) {
        val connection = currentConnection() ?: return@withContext
        val pool = connection.selection.pool
        if (clearedConnection == connection || (pool == ThumbnailPool.NAS && !unmetered.first())) return@withContext
        val disk = diskFor(pool)
        val generation = disk.generation
        for (asset in assets) {
            requireCurrent(connection)
            if (asset.thumbnailUrl.isBlank() || asset.mediaType == "animated_image" || !sameTarget(asset.target, connection.target)) continue
            val request = Preview(asset.id, asset.version, connection, generation)
            try {
                if (prefetchOne(request, disk) == ThumbnailDiskResult.FULL) return@withContext
            } catch (failure: ApiFailure) {
                if (failure.isMissingThumbnail()) continue
                Log.d("ThumbnailCache", "Nearby thumbnail prefetch stopped (HTTP ${failure.status})")
                return@withContext
            } catch (_: IOException) {
                Log.d("ThumbnailCache", "Nearby thumbnail prefetch stopped after an I/O error")
                return@withContext
            }
        }
    }

    private fun selected(pool: ThumbnailPool, current: Connection?): ThumbnailCacheSelection? {
        if (current?.selection?.pool == pool) return current.selection
        return selections.getString(pool.name, null)?.let { ThumbnailCacheSelection(pool, it) }
    }

    suspend fun usage(pool: ThumbnailPool): ThumbnailPoolUsage = withContext(Dispatchers.IO) {
        val selection = selected(pool, currentConnection())
        ThumbnailPoolUsage(selection, selection?.let { diskFor(pool).usage(it.instanceKey) } ?: ThumbnailUsage(0, 0))
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
                // A retained revision record would make the next round skip an empty pool.
                revisionStore.clear()
            }
        } finally {
            diskChanges.update { it + 1 }
        }
    }

    suspend fun localCoverage(): Pair<ThumbnailCacheSelection, ThumbnailCoverage>? {
        val connection = currentConnection()?.takeIf { it.selection.pool == ThumbnailPool.LOCAL } ?: return null
        val progress = provider.api(connection.target).getThumbnailProgress().bodyOrThrow()
        verifyInstance(connection, progress.instanceId)
        requireCurrent(connection)
        return connection.selection to ThumbnailCoverage(progress.thumbsOnDisk, progress.totalAssets)
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
        val disk = diskFor(connection.selection.pool)
        val generation = disk.generation
        val revision = readRevision(connection)
        val serverKey = serverKey(connection)
        if (revision != null && ThumbnailPrefetchGate.decide(
                revision.revision, readLastDone(), serverKey, revision.thumbnailVersion,
            ) == ThumbnailPrefetchDecision.SKIP
        ) {
            val sample = readStoredSample()
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
        recordRound(revision, serverKey, samples)
        publish(connection, ThumbnailPrefetchStatus.Done(cached, total, atCapacity))
    }

    private suspend fun readRevision(connection: Connection): ThumbnailRevisionSnapshot? = try {
        revisions.fetch(connection.target)
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (_: Exception) {
        null
    }

    private fun readLastDone(): ThumbnailPrefetchRecord? = try {
        revisionStore.lastDone()
    } catch (_: Exception) {
        null
    }

    private fun readStoredSample(): Set<ThumbnailPrefetchSample> = try {
        revisionStore.storedSample()
    } catch (_: Exception) {
        emptySet()
    }

    /**
     * Writes the revision read at the start of this round together with a random sample of the
     * entries verified during it. A null revision (old server or parse failure) keeps the old
     * record, so the next round runs full again rather than skipping on stale evidence.
     */
    private fun recordRound(
        revision: ThumbnailRevisionSnapshot?,
        serverKey: String,
        samples: List<ThumbnailPrefetchSample>,
    ) {
        if (revision == null) return
        try {
            revisionStore.setLastDone(
                ThumbnailPrefetchRecord(serverKey, revision.revision, revision.thumbnailVersion),
                ThumbnailPrefetchSampleGate.pickSample(samples),
            )
        } catch (_: Exception) {
            Log.w("ThumbnailCache", "Prefetch revision record could not be saved; the next round will be full")
        }
    }

    private suspend fun prefetchOne(preview: Preview, disk: ThumbnailDiskCache): ThumbnailDiskResult {
        val current = disk.warmState(preview.key, preview.version)
        if (current != ThumbnailDiskResult.MISSING) return current
        return backgroundFetch.withPermit {
            flights.run(preview.flightKey) {
                requireCurrent(preview.connection)
                if (disk.generation != preview.generation || clearedConnection == preview.connection) {
                    throw CancellationException("缩略图缓存已清空")
                }
                val existing = disk.warmState(preview.key, preview.version)
                if (existing != ThumbnailDiskResult.MISSING) return@run existing
                val payload = download(preview, background = true)
                currentCoroutineContext().ensureActive()
                requireCurrent(preview.connection)
                val result = disk.store(preview.key, preview.version, payload.bytes, preview.generation, background = true) {
                    currentConnection() == preview.connection && clearedConnection != preview.connection
                }
                if (result == ThumbnailDiskResult.READY) diskChanges.update { it + 1 }
                result
            }
        }
    }

    private fun publish(connection: Connection, status: ThumbnailPrefetchStatus) {
        if (currentConnection() == connection && clearedConnection != connection) {
            prefetchState.value = ThumbnailPrefetch(connection.selection, status)
        }
    }

    private fun verifyInstance(connection: Connection, instanceId: String?) {
        if (instanceId != connection.target.instanceId) throw ApiFailure(409, "地址已指向另一个媒体库，请重新登录", "instance_changed")
    }

    private data class Payload(val bytes: ByteArray, val verified: Boolean)

    private suspend fun download(preview: Preview, background: Boolean): Payload {
        val target = preview.connection.target
        val url = target.baseUrl.toHttpUrl().newBuilder()
            .addPathSegments("api/v1/assets").addPathSegment(preview.assetId).addPathSegment("thumbnail")
            .addQueryParameter("version", preview.version.toString()).addQueryParameter("size", ThumbnailCachePolicy.SIZE)
            .apply { if (background) addQueryParameter("background", "true") }.build()
        val call = provider.client(target).newCall(Request.Builder().url(url).header("Accept", "image/jpeg").build())
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
                            val buffer = Buffer()
                            val source = body.source()
                            while (true) {
                                val read = source.read(buffer, minOf(8192L, MAX_THUMBNAIL_BYTES + 1 - buffer.size))
                                if (read == -1L) break
                                if (buffer.size > MAX_THUMBNAIL_BYTES) throw IOException("缩略图响应超出读取预算")
                            }
                            val bytes = buffer.readByteArray()
                            if (!validJpeg(bytes)) throw IOException("缩略图正文不是完整 JPEG")
                            Payload(bytes, mediaInstance != null)
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
