package media.qimeng.next

import android.app.Application
import android.net.Uri
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import media.qimeng.next.data.AuthorEntriesConflict
import media.qimeng.next.model.*
import java.util.UUID

data class MaintenanceState(val target: Session? = null, val folder: MaintenanceFolder = MaintenanceFolder(),
    val fragments: List<AuthorFragment> = emptyList(), val cursor: String? = null, val mirror: AuthorMirror? = null,
    val draft: AuthorImportDraft? = null, val missing: MissingAuthorWorks? = null, val summary: AuthorImportSummary? = null,
    val document: LegacyDocument? = null, val plan: MigrationPreview? = null, val mediaRows: List<MigrationRow> = emptyList(),
    val authorRows: List<MigrationRow> = emptyList(), val mediaCursor: String? = null, val authorCursor: String? = null,
    val busy: Boolean = false, val error: String? = null)

class MaintenanceViewModel(application: Application) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    private val mutable = MutableStateFlow(MaintenanceState())
    val state = mutable.asStateFlow()
    private var action: Job? = null
    private var choiceKey: Pair<String?, String>? = null
    private var rebuildKey = UUID.randomUUID().toString()

    init { viewModelScope.launch {
        combine(graph.settings.session, graph.backups.changes) { session, _ -> session }.collect { target ->
            if (target != mutable.value.target) {
                action?.cancel(); choiceKey = null
                mutable.value = MaintenanceState(target = target, folder = target?.let(graph.backups::folder) ?: MaintenanceFolder())
                if (target != null) refreshAuthors()
            } else if (target != null) mutable.update { it.copy(folder = graph.backups.folder(target)) }
        }
    } }
    private fun act(block: suspend (Session) -> Unit) {
        val target = mutable.value.target ?: return
        if (mutable.value.busy) return
        mutable.update { it.copy(busy = true, error = null) }
        action = viewModelScope.launch {
            try { block(target) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (conflict: AuthorEntriesConflict) { if (current(target)) mutable.update { it.copy(missing = conflict.missing) } }
            catch (failure: Exception) { if (current(target)) mutable.update { it.copy(error = libraryError(failure)) } }
            finally { if (current(target)) mutable.update { it.copy(busy = false) } }
        }
    }
    private fun current(target: Session) = sameTarget(target, graph.settings.session.value) && sameTarget(target, mutable.value.target)
    private suspend fun readAuthors(target: Session, more: Boolean = false) {
        val previous = mutable.value
        val result = graph.authorLists.page(target, previous.cursor.takeIf { more })
        val mirror = if (more) previous.mirror else graph.authorLists.mirror(target)
        if (current(target)) mutable.update { it.copy(fragments = ((if (more) previous.fragments else emptyList()) + result.items).distinctBy(AuthorFragment::id),
            cursor = result.nextCursor, mirror = mirror) }
    }
    fun refreshAuthors(more: Boolean = false) = act { readAuthors(it, more) }
    fun bind(uri: Uri, flags: Int) = act { graph.backups.bind(it, uri, flags) }
    fun export() = act { graph.backups.schedule(it, true) }
    fun automatic(on: Boolean) = act { graph.backups.automatic(it, on) }
    fun prepareAuthor(uri: Uri? = null) = act { target ->
        val input = uri ?: graph.backups.currentAuthorFile(target) ?: error("维护目录的当前代尚无作者总表")
        val draft = graph.authorLists.prepare(target, input)
        if (current(target)) { choiceKey = null; mutable.update { it.copy(draft = draft, missing = null, summary = null) } }
    }
    fun dismissDraft() { if (!mutable.value.busy) { choiceKey = null; mutable.update { it.copy(draft = null, missing = null, error = null) } } }
    fun importAuthor(choice: String? = null) = act { target ->
        val draft = mutable.value.draft ?: return@act
        check(sameTarget(target, draft.target)) { "该草稿属于原连接" }
        val key = if (choice == null) draft.key else choiceKey?.takeIf { it.first == choice }?.second ?: UUID.randomUUID().toString().also { choiceKey = choice to it }
        val result = graph.authorLists.applyDraft(draft, choice, key)
        if (current(target)) {
            mutable.update { it.copy(draft = null, missing = null, summary = result) }
            readAuthors(target)
        }
    }
    fun remove(list: AuthorFragment) = act { target ->
        graph.authorLists.remove(target, list, "remove-${list.id}-${list.revision}"); readAuthors(target)
    }
    fun rebuild() = act { target ->
        val result = graph.authorLists.rebuild(target, rebuildKey)
        if (current(target)) { rebuildKey = UUID.randomUUID().toString(); mutable.update { it.copy(summary = result) }; readAuthors(target) }
    }
    fun mirror(initial: AuthorMirror, path: String, fragment: Boolean, listId: String?, key: String) = act { target ->
        val result = graph.authorLists.saveMirror(target, initial, path, fragment, listId, key)
        if (current(target)) mutable.update { it.copy(mirror = result) }
    }
    fun preview(uri: Uri?, source: String, media: Map<String,String>, authors: Map<String,String>) = act { target ->
        val document = if (uri == null) requireNotNull(mutable.value.document) else graph.legacyBackups.stage(target, uri, source)
        if (!current(target)) return@act
        mutable.update { it.copy(document = document, plan = null, mediaRows = emptyList(), authorRows = emptyList()) }
        val plan = graph.legacyBackups.prepare(target, document, media, authors, UUID.randomUUID().toString())
        val items = graph.legacyBackups.rows(target, plan, false)
        val identities = graph.legacyBackups.rows(target, plan, true)
        if (current(target)) mutable.update { it.copy(plan = plan, mediaRows = items.items, mediaCursor = items.nextCursor, authorRows = identities.items, authorCursor = identities.nextCursor) }
    }
    fun nextRows(authors: Boolean) = act { target ->
        val original = mutable.value
        val plan = original.plan ?: return@act
        val cursor = (if (authors) original.authorCursor else original.mediaCursor) ?: return@act
        val result = graph.legacyBackups.rows(target, plan, authors, cursor)
        if (current(target) && mutable.value.plan == plan) mutable.update { if (authors) it.copy(authorRows = it.authorRows + result.items, authorCursor = result.nextCursor)
            else it.copy(mediaRows = it.mediaRows + result.items, mediaCursor = result.nextCursor) }
    }
    fun commit(plan: MigrationPreview, skipped: Boolean) = act { target ->
        check(mutable.value.plan == plan) { "请重新核对导入预检" }
        val result = graph.legacyBackups.commit(target, plan, skipped)
        if (current(target)) mutable.update { it.copy(plan = result) }
    }
    fun resetPreview() { if (!mutable.value.busy) mutable.update { it.copy(document = null, plan = null, mediaRows = emptyList(), authorRows = emptyList(), error = null) } }
}
