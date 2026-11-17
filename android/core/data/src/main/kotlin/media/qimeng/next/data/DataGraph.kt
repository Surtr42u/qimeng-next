package media.qimeng.next.data

import android.content.Context
import kotlinx.coroutines.*

/** Application-scoped composition root shared by foreground UI and WorkManager. */
class DataGraph private constructor(context: Context) {
    val settings = SecureSettings(context)
    internal val provider = ApiProvider(settings)
    val queue = QueueStore(context)
    val media = MediaRepository(settings, provider)
    val uploads = UploadRepository(context, queue, settings)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    init {
        scope.launch {
            try { queue.load(); uploads.schedule() }
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
