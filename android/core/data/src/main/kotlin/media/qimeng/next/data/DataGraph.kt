package media.qimeng.next.data

import android.content.Context
import kotlinx.coroutines.*
import media.qimeng.next.model.Asset
import java.time.Instant

/** Application-scoped composition root shared by foreground UI and WorkManager. */
class DataGraph private constructor(context: Context) {
    val settings = SecureSettings(context)
    internal val provider = ApiProvider(settings)
    val queue = QueueStore(context)
    val media = MediaRepository(settings, provider)
    val analytics = AnalyticsRepository(settings, provider)
    val management = ManagementRepository(settings, provider)
    val aiAccess = AIAccessRepository(settings, provider)
    val authorProfiles = AuthorProfileRepository(context, settings, provider)
    val naming = NamingRepository(settings, provider)
    val uploads = UploadRepository(context, queue, settings)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    /** A normal activity exit must not cancel the final visible segment's journal commit. */
    fun recordEvent(asset: Asset, kind: String, sessionId: String, positionMs: Long = 0, seconds: Double = 0.0,
                    occurredAt: String = Instant.now().toString()) {
        scope.launch {
            try {
                if (queue.state.value == null) queue.load()
                uploads.event(asset, kind, positionMs, sessionId, seconds, occurredAt)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) { queue.reportProblem(safeFailure(failure)) }
        }
    }

    init {
        scope.launch {
            try { queue.load(); uploads.recoverInterruptedIntake(); uploads.schedule() }
            catch (failure: CancellationException) { throw failure }
            catch (_: Exception) { /* QueueStore publishes the recoverable read failure to UI. */ }
        }
    }

    companion object {
        @Volatile private var instance: DataGraph? = null
        fun get(context: Context): DataGraph = instance ?: synchronized(this) {
            instance ?: DataGraph(context.applicationContext).also { instance = it }
        }
    }
}
