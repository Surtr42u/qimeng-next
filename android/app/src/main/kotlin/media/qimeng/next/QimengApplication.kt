package media.qimeng.next

import android.app.Application
import media.qimeng.next.data.DataGraph
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob

class QimengApplication : Application() {
    val graph by lazy { DataGraph.get(this) }
    val runtime by lazy { createRuntime(this) }
    private val runtimeScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    val localConnection by lazy { LocalConnectionCoordinator(graph, runtime, runtimeScope) }

    override fun onCreate() {
        super.onCreate()
        if (runtime.available) localConnection
    }
}
