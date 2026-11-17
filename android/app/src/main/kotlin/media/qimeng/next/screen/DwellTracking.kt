package media.qimeng.next.screen

import android.os.SystemClock
import androidx.compose.runtime.*
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import kotlinx.coroutines.delay
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.Asset
import java.time.Instant

/** Monotonic elapsed time; UTC is only the start timestamp, never a duration clock. */
@Composable
internal fun TrackDwell(asset: Asset, model: LibraryViewModel) {
    val owner = LocalLifecycleOwner.current
    val session = remember(asset.id) { model.viewSessionId() }
    val counter = remember(asset.id, asset.version, owner) {
        DwellCounter(
            started = { model.recordView(asset, session) },
            completed = { seconds, start -> model.recordDwell(asset, session, seconds, start) },
        )
    }
    DisposableEffect(counter, owner) {
        if (owner.lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)) counter.start()
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_RESUME -> counter.start()
                Lifecycle.Event.ON_PAUSE -> counter.stop()
                else -> Unit
            }
        }
        owner.lifecycle.addObserver(observer)
        onDispose { counter.stop(); owner.lifecycle.removeObserver(observer) }
    }
    LaunchedEffect(counter) {
        while (true) { delay(30_000); counter.checkpoint() }
    }
}

internal class DwellCounter(private val started: () -> Unit, private val completed: (Double, String) -> Unit,
                            private val monotonic: () -> Long = SystemClock::elapsedRealtime,
                            private val utc: () -> String = { Instant.now().toString() }) {
    private var monotonicStart: Long? = null
    private var utcStart = ""
    fun start() {
        if (monotonicStart != null) return
        monotonicStart = monotonic()
        utcStart = utc()
        started()
    }
    fun stop() {
        val start = monotonicStart ?: return
        monotonicStart = null
        val seconds = (monotonic() - start) / 1000.0
        if (seconds >= 1.0) completed(seconds, utcStart)
    }
    fun checkpoint() {
        if (monotonicStart == null) return
        stop()
        monotonicStart = monotonic()
        utcStart = utc()
    }
}
