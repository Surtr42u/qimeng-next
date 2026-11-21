package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.model.ExtractionLayer
import media.qimeng.next.model.ExtractionVocabulary
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget
import java.io.IOException
import java.util.UUID

data class ExtractionEditorState(
    val target: Session? = null,
    val base: ExtractionVocabulary? = null,
    val draft: ExtractionLayer? = null,
    val loading: Boolean = false,
    val saving: Boolean = false,
    val conflict: Boolean = false,
    val saved: Boolean = false,
    val error: String? = null,
) {
    val dirty: Boolean get() = draft != null && draft != base?.layer
}

class ExtractionVocabularyViewModel(application: Application) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    private val mutable = MutableStateFlow(ExtractionEditorState())
    val state = mutable.asStateFlow()
    private var operation: Job? = null
    private var transport = 0L
    private var saveKey = UUID.randomUUID().toString()

    fun open(target: Session, revision: Long) {
        if (!sameTarget(target, mutable.value.target) || transport != revision) {
            operation?.cancel()
            transport = revision
            mutable.value = ExtractionEditorState(target = target)
            saveKey = UUID.randomUUID().toString()
        }
        refresh()
    }

    fun refresh(replaceDraft: Boolean = false) {
        val previous = mutable.value
        val target = previous.target ?: return
        if (previous.saving || previous.loading) return
        val connection = transport
        mutable.value = previous.copy(loading = true, error = null)
        operation = viewModelScope.launch {
            try {
                val value = graph.extractionVocabulary.read(target)
                if (!connected(target, connection)) return@launch
                val current = mutable.value
                mutable.value = current.afterRead(value, replaceDraft)
                if (replaceDraft) saveKey = UUID.randomUUID().toString()
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (connected(target, connection)) mutable.value = mutable.value.copy(loading = false, error = explainRead(failure))
            }
        }
    }

    fun edit(transform: (ExtractionLayer) -> ExtractionLayer) {
        val current = mutable.value
        val draft = current.draft ?: return
        if (current.saving) return
        try {
            val updated = transform(draft)
            if (updated == draft) return
            saveKey = UUID.randomUUID().toString()
            mutable.value = current.copy(draft = updated, saved = false, error = null)
        } catch (invalid: IllegalArgumentException) {
            mutable.value = current.copy(error = invalid.message)
        }
    }

    fun discard() {
        val current = mutable.value
        mutable.value = current.copy(draft = current.base?.layer, conflict = false, saved = false, error = null)
        saveKey = UUID.randomUUID().toString()
    }

    fun save() {
        val current = mutable.value
        val target = current.target ?: return
        val base = current.base ?: return
        val draft = current.draft ?: return
        if (current.saving || current.loading || current.conflict || (!current.dirty && base.rebuildState != "failed")) return
        val connection = transport
        val key = saveKey
        mutable.value = current.copy(saving = true, error = null, saved = false)
        operation = viewModelScope.launch {
            try {
                val value = graph.extractionVocabulary.save(target, base.revision, draft, key)
                if (connected(target, connection)) {
                    mutable.value = current.copy(base = value, draft = value.layer, saving = false, saved = true, error = null)
                    saveKey = UUID.randomUUID().toString()
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (!connected(target, connection)) return@launch
                val conflict = failure is ApiFailure && failure.code == "revision_conflict"
                val problem = when {
                    conflict -> null
                    failure is ApiFailure && failure.code == "invalid_source_rules" -> "请核对出处组、角色、别名与停用词的名称和数量。"
                    else -> explainRead(failure)
                }
                mutable.value = mutable.value.copy(saving = false, conflict = conflict, error = problem)
            }
        }
    }

    private fun connected(target: Session, connection: Long) =
        transport == connection && graph.settings.transportRevision.value == connection && sameTarget(target, graph.settings.session.value)

    private fun explainRead(failure: Exception) = when (failure) {
        is ApiFailure -> failure.message
        is IOException -> "无法连接服务器，请重试。"
        else -> "未能读取词表结果，请重试。"
    }
}
