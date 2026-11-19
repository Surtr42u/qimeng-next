package media.qimeng.next.data

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import com.google.gson.JsonParser
import kotlinx.coroutines.runBlocking
import media.qimeng.next.model.EventDeliveryIssue
import media.qimeng.next.model.EventDeliveryKind
import media.qimeng.next.model.PendingEvent
import media.qimeng.next.model.Session
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.util.UUID
import java.util.concurrent.TimeUnit

class ActivityEventQueueTest {
    @Test fun terminalWaitingLegacyAndTransientRecordsLeaveOtherEventsSendableAcrossJournalReload() = runBlocking {
        MockWebServer().use { server ->
            server.start()
            val context = isolatedContext()
            val store = QueueStore(context).also { it.load() }
            val settings = SecureSettings(context)
            val address = server.url("/").toString()
            val origin = Session(address, "instance-a", "A")
            val other = Session(address, "instance-b", "B")
            val activity = ActivityEventQueue(store, settings) {}
            val waiting = event(origin, "waiting")
            val merged = event(origin, "merged")
            val missing = event(origin, "missing")
            val changed = event(origin, "changed")
            val collision = event(origin, "collision")
            val temporary = event(origin, "temporary")
            val proxy = event(origin, "proxy")
            val healthy = event(origin, "healthy")
            val legacy = event(origin, "legacy").copy(failure = "服务器状态已变化或存在重复")
            val elsewhere = event(other, "elsewhere")
            store.update { it.copy(events = listOf(waiting, merged, missing, changed, collision, temporary, proxy, healthy, legacy, elsewhere)) }
            settings.saveSession(origin, "synthetic-a")
            val provider = ApiProvider(settings)
            server.enqueue(rejection(409, "asset_trashed"))
            server.enqueue(rejection(409, "asset_merged"))
            server.enqueue(rejection(404, "not_found"))
            server.enqueue(rejection(409, "content_changed"))
            server.enqueue(rejection(409, "event_conflict"))
            server.enqueue(rejection(503, "unavailable"))
            server.enqueue(MockResponse().setResponseCode(409))
            server.enqueue(accepted(healthy))
            assertTrue(activity.flush(origin, provider.api(origin)))
            listOf(waiting, merged, missing, changed, collision, temporary, proxy, healthy).forEach { assertRequest(server, it) }
            val reopened = QueueStore(context).also { it.load() }
            val recovered = ActivityEventQueue(reopened, settings) {}
            fun saved(event: PendingEvent) = requireNotNull(reopened.state.value).events.single { it.id == event.id }
            assertFalse(requireNotNull(reopened.state.value).events.any { it.id == healthy.id })
            assertEquals(EventDeliveryKind.WAITING, saved(waiting).issue?.kind)
            for ((record, code) in listOf(merged to "asset_merged", missing to "not_found", changed to "content_changed", collision to "event_conflict")) {
                assertEquals(EventDeliveryKind.REJECTED, saved(record).issue?.kind)
                assertEquals(code, saved(record).issue?.code)
            }
            assertEquals(EventDeliveryKind.RETRYABLE, saved(temporary).issue?.kind)
            assertEquals(EventDeliveryKind.UNCONFIRMED, saved(proxy).issue?.kind)
            assertEquals(legacy, saved(legacy))
            assertEquals(elsewhere, saved(elsewhere))

            settings.saveSession(other, "synthetic-b")
            assertTrue(runCatching { recovered.dismiss(origin, saved(merged)) }.isFailure)
            assertTrue(runCatching { recovered.retry(origin, waiting.id) }.isFailure)
            server.enqueue(accepted(elsewhere))
            assertFalse(recovered.flush(other, provider.api(other)))
            assertRequest(server, elsewhere)

            settings.saveSession(origin, "synthetic-a-restored")
            recovered.retry(origin)
            recovered.retry(origin, legacy.id)
            server.enqueue(accepted(waiting))
            server.enqueue(accepted(temporary))
            server.enqueue(rejection(404, "not_found"))
            assertFalse(recovered.flush(origin, provider.api(origin)))
            listOf(waiting, temporary, legacy).forEach { assertRequest(server, it) }
            assertEquals(EventDeliveryKind.REJECTED, saved(legacy).issue?.kind)
            assertEquals(legacy.copy(failure = null), saved(legacy).copy(issue = null))
            recovered.dismiss(origin, saved(merged))
            val remaining = QueueStore(context).also { it.load() }.state.value!!.events
            assertEquals(listOf(missing.id, changed.id, collision.id, proxy.id, legacy.id), remaining.map(PendingEvent::id))
            assertEquals(12, server.requestCount)
        }
    }

    @Test fun aFullLegacyJournalDoesNotBlockAnotherInstanceAndRejectedHistoryHasABoundedAllowance() = runBlocking {
        val context = isolatedContext()
        val store = QueueStore(context).also { it.load() }
        val settings = SecureSettings(context)
        val a = Session("http://127.0.0.1:81/", "library-a", "A")
        val b = Session(a.baseUrl, "library-b", "B")
        val oldRows = List(PENDING_EVENT_LIMIT) { event(a, "old-$it").copy(failure = "旧版只保存了拒绝文字") }
        store.update { it.copy(events = oldRows) }
        val reopened = QueueStore(context).also { it.load() }
        val activity = ActivityEventQueue(reopened, settings) {}
        assertEquals(oldRows, reopened.state.value!!.events)
        assertTrue(runCatching { activity.append(event(a, "full")) }.isFailure)
        val other = event(b, "independent")
        activity.append(other)

        // Simulate typed replies for each old fact. The archive allowance never evicts overflow.
        val rejected = EventDeliveryIssue(EventDeliveryKind.REJECTED, "原媒体已不存在", "not_found")
        reopened.update { queue -> queue.copy(events = queue.events.map {
            if (it.target == a) it.copy(failure = null, issue = rejected) else it
        }) }
        val newlyQueued = List(REJECTED_EVENT_ALLOWANCE) { event(a, "new-$it") }
        reopened.update { it.copy(events = it.events + newlyQueued.dropLast(1)) }
        activity.append(newlyQueued.last())
        assertTrue(runCatching { activity.append(event(a, "upper-bound")) }.isFailure)
        settings.saveSession(a, "synthetic-admin")
        val toReview = reopened.state.value!!.events.first()
        activity.dismiss(a, toReview)
        val replacementSlot = event(a, "usable-after-confirmation")
        activity.append(replacementSlot)
        val finalRows = QueueStore(context).also { it.load() }.state.value!!.events
        assertEquals(oldRows.drop(1).map(PendingEvent::id) + other.id + newlyQueued.map(PendingEvent::id) + replacementSlot.id,
            finalRows.map(PendingEvent::id))
        assertEquals(other, finalRows.single { it.target == b })
    }

    private fun event(target: Session, name: String) = PendingEvent(
        id = "event-$name", target = target, assetId = "asset-$name", kind = "progress", positionMs = 1234,
        contentVersion = 7, sessionId = "session-$name", occurredAt = "2026-01-01T12:30:00Z",
    )

    private fun rejection(status: Int, code: String) = MockResponse().setResponseCode(status)
        .setHeader("Content-Type", "application/json").setBody("""{"code":"$code","message":"synthetic"}""")

    private fun accepted(event: PendingEvent) = MockResponse().setHeader("Content-Type", "application/json").setBody(
        """{"eventId":"${event.id}","kind":"${event.kind}","contentVersion":${event.contentVersion},
            "assetId":"${event.assetId}","createdAt":"${event.occurredAt}"}""",
    )

    private fun assertRequest(server: MockWebServer, event: PendingEvent) {
        val request = requireNotNull(server.takeRequest(5, TimeUnit.SECONDS))
        assertEquals("/api/v1/assets/${event.assetId}/events", request.path)
        val json = JsonParser.parseString(request.body.readUtf8()).asJsonObject
        assertEquals(event.id, json["eventId"].asString)
        assertEquals(event.contentVersion, json["contentVersion"].asLong)
        assertEquals(event.sessionId, json["sessionId"].asString)
        assertEquals(event.occurredAt, json["occurredAt"].asString)
        assertEquals(event.positionMs, json["positionMs"].asLong)
    }

    private fun isolatedContext(): Context {
        val base = InstrumentationRegistry.getInstrumentation().targetContext
        val suffix = UUID.randomUUID().toString()
        val directory = File(base.cacheDir, "events-$suffix").apply { mkdirs() }
        return object : ContextWrapper(base) {
            override fun getFilesDir(): File = directory
            override fun getSharedPreferences(name: String, mode: Int): SharedPreferences = base.getSharedPreferences("$suffix-$name", mode)
        }
    }
}
