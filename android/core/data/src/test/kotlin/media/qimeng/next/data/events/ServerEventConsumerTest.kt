package media.qimeng.next.data.events

import java.io.IOException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import media.qimeng.next.model.Session
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.sse.EventSource
import okhttp3.sse.EventSourceListener
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Two groups: the topic-to-screen mapping and the connection lifecycle.
 * Lifecycle tests drive okhttp callbacks through a fake factory, so no network
 * or Android runtime is involved.
 */
class ServerEventConsumerTest {

    private companion object {
        const val BACKOFF_INITIAL_MS = 3_000L
        val SESSION = Session(baseUrl = "http://nas.example/", instanceId = "instance", label = "owner")
    }

    private class FakeEventSource(private val request: Request) : EventSource {
        var cancelCount = 0
        override fun request(): Request = request
        override fun cancel() {
            cancelCount++
        }
    }

    private class FakeFactory : EventSource.Factory {
        inner class Attempt(val request: Request, val listener: EventSourceListener) {
            val source = FakeEventSource(request)
        }

        val attempts = mutableListOf<Attempt>()

        override fun newEventSource(request: Request, listener: EventSourceListener): EventSource =
            Attempt(request, listener).also { attempts += it }.source
    }

    private fun accepted(request: Request): Response = Response.Builder()
        .request(request)
        .protocol(Protocol.HTTP_1_1)
        .code(200)
        .message("OK")
        .build()

    private fun CoroutineScope.consumer(sessions: MutableStateFlow<Session?>, factory: FakeFactory) =
        ServerEventConsumer(sessions, { factory }, this)

    @Test
    fun `events map to the data they invalidate and unknown frames are ignored`() {
        val signals = ServerEventSignals()
        assertEquals(0L, signals.library.value)

        signals.received(ServerEventTopics.LIBRARY_CHANGED)
        signals.received(ServerEventTopics.UPLOAD_DONE)
        assertEquals(2L, signals.library.value)
        assertEquals(0L, signals.favorite.value)

        signals.received(ServerEventTopics.FAVORITE_CHANGED)
        assertEquals(1L, signals.favorite.value)
        assertEquals(0L, signals.like.value)

        signals.received(ServerEventTopics.LIKE_CHANGED)
        assertEquals(1L, signals.like.value)

        signals.received("hello")
        signals.received("scan.progress")
        signals.received(null)
        assertEquals(2L, signals.library.value)
        assertEquals(1L, signals.favorite.value)
        assertEquals(1L, signals.like.value)
    }

    @Test
    fun `opening a stream invalidates every domain once`() {
        val signals = ServerEventSignals()
        signals.streamOpened()
        assertEquals(1L, signals.library.value)
        assertEquals(1L, signals.favorite.value)
        assertEquals(1L, signals.like.value)
    }

    @Test
    fun `connection starts only when visible with a session and stops on logout`() = runTest {
        val sessions = MutableStateFlow<Session?>(SESSION)
        val factory = FakeFactory()
        val consumer = CoroutineScope(UnconfinedTestDispatcher(testScheduler) + SupervisorJob())
            .consumer(sessions, factory)

        consumer.start()
        advanceUntilIdle()
        assertEquals("no connection while the app is hidden", 0, factory.attempts.size)

        consumer.setForeground(true)
        advanceUntilIdle()
        val attempt = factory.attempts.single()
        assertEquals("http://nas.example/api/v1/events", attempt.request.url.toString())

        val opened = attempt.listener
        opened.onOpen(attempt.source, accepted(attempt.request))
        advanceUntilIdle()
        assertTrue("a successful open invalidates every domain", consumer.signals.library.value > 0)

        sessions.value = null
        advanceUntilIdle()
        assertEquals("logging out cancels the active stream", 1, attempt.source.cancelCount)
        assertEquals(1, factory.attempts.size)
        consumer.stop()
    }

    @Test
    fun `stream close backs off three seconds then six and a successful open resets`() = runTest {
        val sessions = MutableStateFlow<Session?>(SESSION)
        val factory = FakeFactory()
        val consumer = CoroutineScope(UnconfinedTestDispatcher(testScheduler) + SupervisorJob())
            .consumer(sessions, factory)
        consumer.setForeground(true)
        consumer.start()
        advanceUntilIdle()

        factory.attempts[0].listener.onFailure(factory.attempts[0].source, IOException("dropped"), null)
        advanceTimeBy(BACKOFF_INITIAL_MS - 1)
        runCurrent()
        assertEquals("no retry before the backoff elapses", 1, factory.attempts.size)

        advanceTimeBy(1)
        runCurrent()
        assertEquals("first retry after three seconds", 2, factory.attempts.size)

        factory.attempts[1].listener.onFailure(factory.attempts[1].source, IOException("dropped"), null)
        advanceTimeBy(BACKOFF_INITIAL_MS * 2 - 1)
        runCurrent()
        assertEquals("the second wait doubles", 2, factory.attempts.size)
        advanceTimeBy(1)
        runCurrent()
        assertEquals(3, factory.attempts.size)

        // A stream that opens resets the delay to the initial value.
        factory.attempts[2].listener.onOpen(factory.attempts[2].source, accepted(factory.attempts[2].request))
        advanceUntilIdle()
        factory.attempts[2].listener.onClosed(factory.attempts[2].source)
        advanceTimeBy(BACKOFF_INITIAL_MS)
        runCurrent()
        assertEquals("a reopened stream waits the initial delay again", 4, factory.attempts.size)
        consumer.stop()
    }

    @Test
    fun `backgrounding cancels the connection without scheduling a replacement`() = runTest {
        val sessions = MutableStateFlow<Session?>(SESSION)
        val factory = FakeFactory()
        val consumer = CoroutineScope(UnconfinedTestDispatcher(testScheduler) + SupervisorJob())
            .consumer(sessions, factory)
        consumer.setForeground(true)
        consumer.start()
        advanceUntilIdle()
        val attempt = factory.attempts.single()
        attempt.listener.onOpen(attempt.source, accepted(attempt.request))
        advanceUntilIdle()

        consumer.setForeground(false)
        advanceUntilIdle()
        assertEquals(1, attempt.source.cancelCount)
        advanceTimeBy(60_000)
        runCurrent()
        assertEquals("a hidden app must not retry", 1, factory.attempts.size)
        consumer.stop()
    }
}
