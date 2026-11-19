package media.qimeng.next.data

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import android.graphics.Bitmap
import android.graphics.Color
import androidx.test.platform.app.InstrumentationRegistry
import coil3.ImageLoader
import coil3.decode.DataSource
import coil3.request.CachePolicy
import coil3.request.ImageRequest
import coil3.request.SuccessResult
import kotlinx.coroutines.async
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import media.qimeng.next.model.Asset
import media.qimeng.next.model.Session
import media.qimeng.next.model.ThumbnailCoverage
import media.qimeng.next.model.ThumbnailPool
import media.qimeng.next.model.ThumbnailPrefetchStatus
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import okio.Buffer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.io.File
import java.util.Base64
import java.util.UUID
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

class ThumbnailCacheTest {
    @Test fun prefetchPaginatesOnUnmeteredNetworkAndSharesVersionedDiskWithCoil() = runBlocking {
        val fixture = Fixture()
        val version = AtomicInteger(1)
        val catalogRequests = AtomicInteger()
        val previewRequests = AtomicInteger()
        val server = MockWebServer().apply {
            dispatcher = object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse {
                    if (request.getHeader("Authorization") != "Bearer $OWNER") return MockResponse().setResponseCode(401)
                    val url = requireNotNull(request.requestUrl)
                    return when {
                        url.encodedPath == "/api/v1/thumbnails/candidates" -> {
                            catalogRequests.incrementAndGet()
                            when (url.queryParameter("cursor")) {
                                null -> catalog("nas", """{"assetId":"a","contentVersion":${version.get()}}""", 2, "next-fixture")
                                "next-fixture" -> catalog("nas", """{"assetId":"z","contentVersion":1}""")
                                else -> MockResponse().setResponseCode(400)
                            }
                        }
                        url.encodedPath == "/api/v1/assets/a/thumbnail" && url.queryParameter("version") == version.get().toString() ->
                            fixture.jpegResponse("nas", previewRequests.incrementAndGet())
                        url.encodedPath == "/api/v1/assets/z/thumbnail" && url.queryParameter("version") == "1" ->
                            fixture.jpegResponse("nas", previewRequests.incrementAndGet())
                        else -> MockResponse().setResponseCode(409)
                    }
                }
            }
            start()
        }
        try {
            val session = fixture.select(server, "nas")
            var job = launch { fixture.repository.prefetchWhileStarted() }
            fixture.awaitStatus { it == ThumbnailPrefetchStatus.WaitingForNetwork }
            assertEquals(0, server.requestCount)
            fixture.unmetered.value = true
            fixture.awaitCached(2)
            assertEquals(2, catalogRequests.get())
            assertEquals(2, previewRequests.get())
            val usage = fixture.repository.usage(ThumbnailPool.NAS)
            assertEquals(2L, usage.usage.files)
            assertTrue(usage.usage.bytes > fixture.jpegBytes.size * 2)
            assertEquals(DataSource.DISK, fixture.coil(asset(session, "a", 1)).dataSource)
            assertEquals(2, previewRequests.get())

            job.cancelAndJoin()
            fixture.reopen()
            job = launch { fixture.repository.prefetchWhileStarted() }
            fixture.awaitCached(2)
            assertEquals(4, catalogRequests.get())
            assertEquals("A cold process reused both files", 2, previewRequests.get())
            assertEquals(DataSource.DISK, fixture.coil(asset(session, "z", 1)).dataSource)
            job.cancelAndJoin()

            fixture.reopen(remove = {
                val instance = requireNotNull(usage.selection).instanceKey
                val entry = thumbnailHash("$instance:${thumbnailHash("z")}")
                assertTrue(File(fixture.directory, "nas/$entry.1").delete())
            })
            version.set(2)
            job = launch { fixture.repository.prefetchWhileStarted() }
            fixture.awaitCached(2)
            assertEquals("The changed version and missing file were repaired", 4, previewRequests.get())
            assertEquals(DataSource.DISK, fixture.coil(asset(session, "a", 2)).dataSource)
            assertFalse(fixture.load(asset(session, "a", 1)) is SuccessResult)
            assertEquals("Only current objects occupy the pool", 2L, fixture.repository.usage(ThumbnailPool.NAS).usage.files)
            job.cancelAndJoin()
        } finally {
            server.shutdown()
            fixture.close()
        }
    }

    @Test fun pinnedLocalPrefetchIgnoresMeteringAndKeepsOtherInstancesWhenClearing() = runBlocking {
        val fixture = Fixture()
        val currentNas = MutableStateFlow("nas-a")
        val nas = MockWebServer().apply {
            dispatcher = object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse = reply(fixture, request, currentNas.value)
            }
            start()
        }
        val cert = HeldCertificate.Builder().commonName("Synthetic embedded service")
            .addSubjectAlternativeName("localhost").build()
        val tls = HandshakeCertificates.Builder().heldCertificate(cert).build()
        val local = MockWebServer().apply {
            useHttps(tls.sslSocketFactory(), false)
            dispatcher = object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse = reply(fixture, request, "local")
            }
            start()
        }
        try {
            val first = fixture.select(nas, "nas-a")
            assertEquals(DataSource.NETWORK, fixture.coil(asset(first, "asset", 1)).dataSource)
            val firstSelection = requireNotNull(fixture.repository.usage(ThumbnailPool.NAS).selection)
            currentNas.value = "nas-b"
            val second = fixture.select(nas, "nas-b")
            assertEquals(0L, fixture.repository.usage(ThumbnailPool.NAS).usage.files)
            assertEquals(DataSource.NETWORK, fixture.coil(asset(second, "asset", 1)).dataSource)
            assertFalse(fixture.load(asset(first, "asset", 1)) is SuccessResult)
            assertTrue(runCatching { fixture.repository.clear(firstSelection) }.isFailure)
            val job = launch { fixture.repository.prefetchWhileStarted() }
            fixture.awaitStatus { it == ThumbnailPrefetchStatus.WaitingForNetwork }

            val pinned = Base64.getEncoder().encodeToString(cert.certificate.encoded)
            val selectedLocal = fixture.select(local, "local", pinned)
            fixture.awaitCached(1)
            val localUsage = fixture.repository.usage(ThumbnailPool.LOCAL)
            assertEquals(1L, localUsage.usage.files)
            assertEquals(localUsage.selection to ThumbnailCoverage(3, 4), fixture.repository.localCoverage())
            assertEquals(DataSource.DISK, fixture.coil(asset(selectedLocal, "asset", 1)).dataSource)
            val nasUsage = fixture.repository.usage(ThumbnailPool.NAS)
            assertEquals(1L, nasUsage.usage.files)
            fixture.repository.clear(requireNotNull(localUsage.selection))
            assertEquals(0L, fixture.repository.usage(ThumbnailPool.LOCAL).usage.files)
            assertEquals(nasUsage, fixture.repository.usage(ThumbnailPool.NAS))
            fixture.repository.clear(requireNotNull(nasUsage.selection))
            job.cancelAndJoin()

            currentNas.value = "nas-a"
            fixture.select(nas, "nas-a")
            assertEquals(DataSource.DISK, fixture.coil(asset(first, "asset", 1)).dataSource)
            assertEquals(1L, fixture.repository.usage(ThumbnailPool.NAS).usage.files)
        } finally {
            nas.shutdown()
            local.shutdown()
            fixture.close()
        }
    }

    @Test fun clearAndSwitchRejectLateWritesAndOnlyAcceptDerivedJpegs() = runBlocking {
        val fixture = Fixture()
        val arrived = CountDownLatch(2)
        val releaseFirst = CountDownLatch(1)
        val releaseSecond = CountDownLatch(1)
        val calls = AtomicInteger()
        val server = MockWebServer().apply {
            dispatcher = object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse {
                    if (request.getHeader("Authorization") != "Bearer $OWNER") return MockResponse().setResponseCode(401)
                    return when (request.requestUrl?.encodedPath) {
                        "/api/v1/assets/first/thumbnail", "/api/v1/assets/second/thumbnail" -> {
                            val index = calls.incrementAndGet()
                            arrived.countDown()
                            val release = if (index == 1) releaseFirst else releaseSecond
                            check(release.await(10, TimeUnit.SECONDS))
                            fixture.jpegResponse("old", index)
                        }
                        "/api/v1/assets/text/thumbnail" -> MockResponse().setHeader("Content-Type", "text/plain")
                            .setHeader("X-Qimeng-Instance-Id", "old").setBody("synthetic non-image content")
                        else -> MockResponse().setResponseCode(404)
                    }
                }
            }
            start()
        }
        try {
            val old = fixture.select(server, "old")
            val first = async { fixture.load(asset(old, "first", 1)) }
            await { arrived.count == 1L }
            fixture.repository.clear(requireNotNull(fixture.repository.usage(ThumbnailPool.NAS).selection))
            releaseFirst.countDown()
            assertTrue(first.await() is SuccessResult)
            assertEquals(0L, fixture.repository.usage(ThumbnailPool.NAS).usage.files)
            assertFalse(fixture.load(asset(old, "text", 1).copy(thumbnailUrl = "/api/v1/assets/text/content")) is SuccessResult)
            assertEquals(0L, fixture.repository.usage(ThumbnailPool.NAS).usage.files)

            val second = async { runCatching { fixture.load(asset(old, "second", 1)) }.getOrNull() }
            await { arrived.count == 0L }
            val replacement = fixture.select(server, "replacement")
            releaseSecond.countDown()
            assertFalse(second.await() is SuccessResult)
            assertEquals(0L, fixture.repository.usage(ThumbnailPool.NAS).usage.files)
            assertFalse(fixture.load(asset(replacement, "text", 1)) is SuccessResult)
            fixture.select(server, "old")
            assertEquals("Late results were discarded from the old instance", 0L, fixture.repository.usage(ThumbnailPool.NAS).usage.files)
        } finally {
            releaseFirst.countDown()
            releaseSecond.countDown()
            server.shutdown()
            fixture.close()
        }
    }

    private class Fixture {
        private val context = isolatedContext()
        val directory = File(context.cacheDir, "test-thumbnails")
        val unmetered = MutableStateFlow(false)
        var settings = SecureSettings(context)
        var repository = repository()
        private var loader = loader()
        val jpegBytes = ByteArrayOutputStream().run {
            val bitmap = Bitmap.createBitmap(12, 10, Bitmap.Config.ARGB_8888)
            bitmap.eraseColor(Color.BLUE)
            check(bitmap.compress(Bitmap.CompressFormat.JPEG, 90, this))
            bitmap.recycle()
            toByteArray()
        }

        private fun repository() = ThumbnailCacheRepository(context, settings, ApiProvider(settings), unmetered, directory)
        private fun loader() = ImageLoader.Builder(context).components { repository.addImageComponents(this) }.build()

        fun select(server: MockWebServer, id: String, certificate: String? = null): Session =
            Session(server.url("/").toString(), id, "Synthetic library").also { settings.saveSession(it, OWNER, certificate) }

        fun reopen(remove: () -> Unit = {}) {
            loader.shutdown()
            repository.closeForTest()
            remove()
            settings = SecureSettings(context)
            repository = repository()
            loader = loader()
        }

        suspend fun load(asset: Asset) =
            loader.execute(ImageRequest.Builder(context).data(asset).memoryCachePolicy(CachePolicy.DISABLED).build())

        suspend fun coil(asset: Asset) = load(asset) as SuccessResult

        suspend fun awaitStatus(match: (ThumbnailPrefetchStatus) -> Boolean) {
            withTimeout(12_000L) { repository.prefetch.first { it?.let { match(it.status) } == true } }
        }

        suspend fun awaitCached(files: Long) = awaitStatus { it is ThumbnailPrefetchStatus.Done && it.cached == files }

        fun jpegResponse(instance: String, sequence: Int) = MockResponse().setHeader("Content-Type", "image/jpeg")
            .setHeader("X-Qimeng-Instance-Id", instance).setHeader("ETag", "fixture-$sequence").setBody(Buffer().write(jpegBytes))

        fun close() {
            loader.shutdown()
            repository.closeForTest()
            context.cacheDir.deleteRecursively()
        }
    }

    companion object {
        private const val OWNER = "synthetic-thumbnail-owner"

        private fun isolatedContext(): Context {
            val base = InstrumentationRegistry.getInstrumentation().targetContext
            val suffix = UUID.randomUUID().toString()
            val directory = File(base.cacheDir, "thumbnails-$suffix").apply { mkdirs() }
            return object : ContextWrapper(base) {
                override fun getCacheDir(): File = directory
                override fun getSharedPreferences(name: String, mode: Int): SharedPreferences =
                    base.getSharedPreferences("$suffix-$name", mode)
            }
        }

        private fun catalog(instance: String, items: String, total: Int? = null, next: String? = null): MockResponse {
            val fields = buildList {
                add(""""instanceId":"$instance"""")
                add(""""items":[$items]""")
                if (total != null) add(""""totalMatched":$total""")
                if (next != null) add(""""nextCursor":"$next"""")
            }
            return json(fields.joinToString(prefix = "{", postfix = "}"))
        }

        private fun json(body: String) = MockResponse().setHeader("Content-Type", "application/json").setBody(body)

        private fun reply(fixture: Fixture, request: RecordedRequest, id: String): MockResponse {
            if (request.getHeader("Authorization") != "Bearer $OWNER") return MockResponse().setResponseCode(401)
            return when (request.requestUrl?.encodedPath) {
                "/api/v1/thumbnails/candidates" -> catalog(id, """{"assetId":"asset","contentVersion":1}""", 1)
                "/api/v1/thumbnails/progress" -> json("""{"instanceId":"$id","thumbsOnDisk":3,"totalAssets":4}""")
                "/api/v1/assets/asset/thumbnail" -> fixture.jpegResponse(id, 1)
                else -> MockResponse().setResponseCode(404)
            }
        }

        private suspend fun await(ready: () -> Boolean) {
            withTimeout(10_000L) {
                while (!ready()) kotlinx.coroutines.delay(10L)
            }
        }

        private fun asset(target: Session, id: String, version: Long) = Asset(
            id = id, libraryId = "library", target = target, name = "$id.png", originalName = "$id.png",
            relativePath = "$id.png", mediaType = "image", version = version, revision = 1, tagsRevision = 1,
            bytes = 256, width = 12, height = 10, durationMs = null, authorId = null, authorIds = emptyList(),
            authorsRevision = 1, authorName = null, tags = emptyList(), favorite = false, liked = false, likeCount = 0,
            positionMs = 0, positionVersion = version, note = "", sourceIds = emptyList(), characterIds = emptyList(),
            acquisitionSources = emptyList(), collectionId = null, page = null,
            contentUrl = "${target.baseUrl}api/v1/assets/$id/content?version=$version",
            thumbnailUrl = "${target.baseUrl}api/v1/assets/$id/thumbnail?version=$version", addedAt = "2026-01-01T00:00:00Z",
        )
    }
}
