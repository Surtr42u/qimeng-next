package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget

class MediaFormLookupsViewModel(application: Application) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph

    suspend fun authors(target: Session, transport: Long, query: String, cursor: String?) =
        graph.media.authors(cursor = cursor, query = query.takeIf(String::isNotBlank), kind = "regular", limit = 20, target = target)
            .also { verifyTarget(target, transport) }

    suspend fun sources(target: Session, transport: Long) =
        graph.media.acquisitionChoices(target).also { verifyTarget(target, transport) }

    suspend fun fileStems(target: Session, transport: Long, libraryId: String, directory: String, prefix: String, numberClass: String) =
        graph.naming.fileStems(target, libraryId, directory, prefix, numberClass).also { verifyTarget(target, transport) }

    private fun verifyTarget(target: Session, transport: Long) {
        check(transport == graph.settings.transportRevision.value && sameTarget(target, graph.settings.session.value)) { "连接已改变" }
    }
}
