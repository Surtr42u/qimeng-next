package media.qimeng.next

import android.app.Application
import media.qimeng.next.data.DataGraph

class QimengApplication : Application() {
    val graph by lazy { DataGraph.get(this) }
}
