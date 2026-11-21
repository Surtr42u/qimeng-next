package media.qimeng.next.data.events

import android.app.Activity
import android.app.Application
import android.content.Context
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import kotlin.coroutines.coroutineContext
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import media.qimeng.next.model.Session
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Request
import okhttp3.Response
import okhttp3.sse.EventSource
import okhttp3.sse.EventSourceListener

/**
 * Keeps one `text/event-stream` connection open while a session exists and the
 * app is visible, and delivers invalidation counters through [signals].
 *
 * The stream is best effort, never business state: a dropped connection only
 * delays refresh until a reconnect, and the reconnect always re-reads the
 * authority. Events missed during the gap are not replayed.
 *
 * Lifecycle: [setForeground] comes from real activity visibility (see
 * [ActivityVisibility]); [start] observes the selected session. A logged-out
 * session or a backgrounded app cancels the connection. Every failed or closed
 * stream backs off, doubling up to [RECONNECT_BACKOFF_MAX_MS], so a server that
 * accepts and immediately closes cannot turn into a reconnect storm.
 */
class ServerEventConsumer internal constructor(
    private val sessions: StateFlow<Session?>,
    private val factoryFor: (Session) -> EventSource.Factory,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.IO),
) {

    /** Screen-facing invalidation counters; the only event-to-refresh mapping. */
    val signals = ServerEventSignals()

    private val foreground = MutableStateFlow(false)
    private var observer: Job? = null

    /** Starts the connection driver once. A second call is a no-op. */
    fun start() {
        synchronized(this) {
            if (observer != null) return
            observer = scope.launch {
                combine(sessions, foreground) { session, visible -> if (visible) session else null }
                    .distinctUntilChanged()
                    .collectLatest { target ->
                        if (target != null) consume(target)
                    }
            }
        }
    }

    /** Stops the driver and the active connection. Safe to call repeatedly. */
    fun stop() {
        synchronized(this) {
            observer?.cancel()
            observer = null
        }
    }

    /** Visibility changes come from the activity lifecycle, never from screen state. */
    fun setForeground(visible: Boolean) {
        foreground.value = visible
    }

    /** Internal seam for unit tests: the listener bridging okhttp callbacks to signals. */
    internal fun listener(opened: CompletableDeferred<Boolean>, closed: CompletableDeferred<Unit>): EventSourceListener =
        object : EventSourceListener() {
            override fun onOpen(eventSource: EventSource, response: Response) {
                opened.complete(true)
                // A reconnect may have missed frames; invalidate once and let
                // the screens re-read authoritative data.
                signals.streamOpened()
            }

            override fun onEvent(eventSource: EventSource, id: String?, type: String?, data: String) {
                // Payloads are change signals, not state. Only the topic decides
                // which data is stale.
                signals.received(type)
            }

            override fun onClosed(eventSource: EventSource) {
                closed.complete(Unit)
            }

            override fun onFailure(eventSource: EventSource, t: Throwable?, response: Response?) {
                if (!opened.isCompleted) opened.complete(false)
                closed.complete(Unit)
            }
        }

    private suspend fun consume(target: Session) {
        val request = try {
            Request.Builder().url(target.baseUrl.toHttpUrl().resolve(ServerEventTopics.PATH)
                ?: error("事件流地址无效")).build()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Exception) {
            // An unusable address is a configuration problem, not a transient
            // failure; wait for a session change instead of retrying.
            return
        }
        var backoff = RECONNECT_BACKOFF_INITIAL_MS
        while (coroutineContext.isActive) {
            val factory = try {
                factoryFor(target)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                // The credential or certificate for this session is unusable.
                // The session flow drives the next attempt.
                return
            }
            val opened = CompletableDeferred<Boolean>()
            val closed = CompletableDeferred<Unit>()
            val source = try {
                factory.newEventSource(request, listener(opened, closed))
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                null
            }
            if (source != null) {
                try {
                    if (opened.await()) {
                        backoff = RECONNECT_BACKOFF_INITIAL_MS
                        closed.await()
                    }
                } finally {
                    source.cancel()
                }
            }
            if (!coroutineContext.isActive) break
            delay(backoff)
            backoff = (backoff * 2).coerceAtMost(RECONNECT_BACKOFF_MAX_MS)
        }
    }

    private companion object {
        /**
         * Matches the server's retry hint: a restart window is usually seconds,
         * so the first reconnect should succeed.
         */
        const val RECONNECT_BACKOFF_INITIAL_MS = 3_000L

        /** A long outage costs at most one attempt per 30 seconds. */
        const val RECONNECT_BACKOFF_MAX_MS = 30_000L
    }
}

/**
 * Turns activity visibility into one boolean. Configuration changes briefly
 * stop and restart activities, so hiding is delayed by a short grace period
 * instead of tearing the stream down for every rotation.
 */
internal class ActivityVisibility(
    context: Context,
    private val onChange: (Boolean) -> Unit,
) : Application.ActivityLifecycleCallbacks {

    private val app = context.applicationContext as? Application
    private val handler = Handler(Looper.getMainLooper())
    private var started = 0
    private var visible = false
    private val hide = Runnable {
        if (started == 0 && visible) {
            visible = false
            onChange(false)
        }
    }

    fun register() {
        app?.registerActivityLifecycleCallbacks(this)
    }

    override fun onActivityStarted(activity: Activity) {
        started++
        handler.removeCallbacks(hide)
        if (!visible) {
            visible = true
            onChange(true)
        }
    }

    override fun onActivityStopped(activity: Activity) {
        started = (started - 1).coerceAtLeast(0)
        if (started == 0) handler.postDelayed(hide, BACKGROUND_GRACE_MS)
    }

    override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) = Unit
    override fun onActivityResumed(activity: Activity) = Unit
    override fun onActivityPaused(activity: Activity) = Unit
    override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) = Unit
    override fun onActivityDestroyed(activity: Activity) = Unit

    private companion object {
        const val BACKGROUND_GRACE_MS = 1_500L
    }
}
