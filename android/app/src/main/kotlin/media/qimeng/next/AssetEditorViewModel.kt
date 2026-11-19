package media.qimeng.next

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import media.qimeng.next.data.ApiFailure
import media.qimeng.next.model.Asset
import media.qimeng.next.model.Author
import media.qimeng.next.model.Session
import media.qimeng.next.model.acquisitionTerms
import media.qimeng.next.model.lockedFileExtension
import media.qimeng.next.model.sameTarget
import media.qimeng.next.model.validFileStem

data class AssetEditorTarget(val session: Session, val transport: Long, val id: String)

data class AssetEditorState(
    val target: AssetEditorTarget? = null,
    val asset: Asset? = null,
    val stem: String = "",
    val authorIds: List<String> = emptyList(),
    val authors: Map<String, Author> = emptyMap(),
    val newNames: List<String> = emptyList(),
    val sourceText: String = "",
    val authorSourceText: Map<String, String> = emptyMap(),
    val loading: Boolean = false,
    val saving: Boolean = false,
    val saved: Boolean = false,
    val conflict: Boolean = false,
    val error: String? = null,
    val savedSteps: List<String> = emptyList(),
) {
    val extension: String get() = asset?.let { lockedFileExtension(it.name) }.orEmpty()
    val nameChanged: Boolean get() = asset != null && stem.trim() + extension != asset.name
    val authorsChanged: Boolean get() = asset != null && (authorIds != asset.editableAuthorIds() || newNames.isNotEmpty())
    val sourcesChanged: Boolean get() = asset != null && acquisitionTerms(sourceText) != asset.acquisitionSources
    val changedAuthorSources: List<Author> get() = authorIds.mapNotNull(authors::get).filter {
        authorSourceText[it.id]?.let(::acquisitionTerms)?.let { edited -> edited != it.acquisitionSources } == true
    }
    val dirty: Boolean get() = nameChanged || authorsChanged || sourcesChanged || changedAuthorSources.isNotEmpty()
    val canSave: Boolean get() = dirty && !conflict && !loading && !saving && validFileStem(stem, extension) && authorIds.size + newNames.size <= 20
}

class AssetEditorViewModel(application: Application) : AndroidViewModel(application) {
    private val graph = (application as QimengApplication).graph
    private val mutable = MutableStateFlow(AssetEditorState())
    val state = mutable.asStateFlow()
    private var operation: Job? = null

    fun open(target: AssetEditorTarget) {
        if (mutable.value.target == target) return
        operation?.cancel()
        mutable.value = AssetEditorState(target = target)
        reload()
    }

    fun reload() {
        val previous = mutable.value
        val target = previous.target ?: return
        if (previous.loading || previous.saving) return
        mutable.value = previous.copy(loading = true, error = null)
        operation = viewModelScope.launch {
            try {
                val asset = graph.media.asset(target.id, target.session)
                check(asset.id == target.id) { "原媒体已合并，请返回并打开代表作品。" }
                val ids = asset.editableAuthorIds()
                val authors = coroutineScope { ids.map { id -> async { graph.media.author(id, target.session) } }.awaitAll() }
                if (!isCurrent(target)) return@launch
                mutable.value = AssetEditorState(target = target, asset = asset,
                    stem = asset.name.removeSuffix(lockedFileExtension(asset.name)), authorIds = ids, authors = authors.associateBy(Author::id),
                    sourceText = asset.acquisitionSources.joinToString("\n"))
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (isCurrent(target)) mutable.value = previous.copy(error = libraryError(failure))
            }
        }
    }

    fun setStem(value: String) = edit { it.copy(stem = value) }
    fun setAssetSources(value: String) = edit { it.copy(sourceText = value) }
    fun setAuthorSources(id: String, value: String) = edit { it.copy(authorSourceText = it.authorSourceText + (id to value)) }

    fun pickAuthor(author: Author) = edit {
        if (author.id in it.authorIds || it.authorIds.size + it.newNames.size >= 20) return@edit it
        it.copy(authorIds = it.authorIds + author.id, authors = it.authors + (author.id to (it.authors[author.id] ?: author)))
    }

    fun removeAuthor(id: String) = edit {
        it.copy(authorIds = it.authorIds - id, authorSourceText = it.authorSourceText - id)
    }

    fun addName(value: String) = edit {
        if (value in it.newNames || it.authorIds.size + it.newNames.size >= 20) return@edit it
        it.copy(newNames = it.newNames + value)
    }

    fun removeName(value: String) = edit { it.copy(newNames = it.newNames - value) }

    private fun edit(transform: (AssetEditorState) -> AssetEditorState) {
        val current = mutable.value
        if (!current.loading && !current.saving && current.asset != null) mutable.value = transform(current).copy(error = current.error.takeIf { current.conflict })
    }

    fun save() {
        val initial = mutable.value
        val target = initial.target ?: return
        var asset = initial.asset ?: return
        if (!initial.canSave || !isCurrent(target)) return
        mutable.value = initial.copy(saving = true, error = null, saved = false)
        operation = viewModelScope.launch {
            var stage = "媒体状态"
            try {
                if (initial.nameChanged) {
                    stage = "文件名"
                    val name = initial.stem.trim() + initial.extension
                    val path = listOf(asset.relativePath.substringBeforeLast('/', ""), name).filter(String::isNotEmpty).joinToString("/")
                    asset = graph.media.rename(asset, path)
                    applied(target, stage) { it.copy(asset = asset, stem = asset.name.removeSuffix(it.extension)) }
                }
                if (initial.authorsChanged || initial.sourcesChanged) {
                    stage = if (initial.authorsChanged && initial.sourcesChanged) "作者关联与媒体来源"
                        else if (initial.authorsChanged) "作者关联" else "媒体来源"
                    asset = graph.media.updateEditableAsset(asset, initial.authorIds.takeIf { initial.authorsChanged }, initial.newNames,
                        acquisitionTerms(initial.sourceText).takeIf { initial.sourcesChanged })
                    applied(target, stage) { it.copy(asset = asset, authorIds = asset.editableAuthorIds(),
                        newNames = emptyList(), sourceText = asset.acquisitionSources.joinToString("\n")) }
                }
                if (!initial.nameChanged && !initial.authorsChanged && !initial.sourcesChanged) {
                    val fresh = graph.media.asset(asset.id, target.session)
                    if (fresh.id != asset.id || fresh.revision != asset.revision) throw ApiFailure(409, "媒体已在服务端修改。", "revision_conflict")
                }
                for (author in initial.changedAuthorSources) {
                    stage = "${author.name}的来源"
                    val sources = acquisitionTerms(initial.authorSourceText.getValue(author.id))
                    val updated = graph.media.saveAuthorSources(target.session, author, sources)
                    applied(target, stage) { it.copy(authors = it.authors + (updated.id to updated), authorSourceText = it.authorSourceText - updated.id) }
                }
                if (isCurrent(target)) mutable.value = mutable.value.copy(saving = false, saved = true)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: Exception) {
                if (!isCurrent(target)) return@launch
                val code = (failure as? ApiFailure)?.code
                val conflict = code in setOf("revision_conflict", "asset_merged", "asset_trashed", "not_found")
                val message = when (code) {
                    "revision_conflict" -> "服务端已修改，请重新读取。待保存的输入仍保留。"
                    "asset_merged" -> "原媒体已合并，请返回并打开代表作品。"
                    "asset_trashed" -> "媒体在回收站，恢复后可重新编辑。"
                    "path_occupied" -> "这个名称已被占用，请输入其他名字。"
                    else -> libraryError(failure)
                }
                mutable.value = mutable.value.copy(saving = false, conflict = conflict, error = "$stage：$message")
            }
        }
    }

    private fun applied(target: AssetEditorTarget, stage: String, update: (AssetEditorState) -> AssetEditorState) {
        if (!isCurrent(target)) throw CancellationException()
        val current = mutable.value
        mutable.value = update(current).copy(savedSteps = (current.savedSteps + stage).distinct())
    }

    fun close() {
        operation?.cancel()
        mutable.value = AssetEditorState()
    }

    private fun isCurrent(target: AssetEditorTarget) = mutable.value.target == target &&
        target.transport == graph.settings.transportRevision.value && sameTarget(target.session, graph.settings.session.value)
}

private fun Asset.editableAuthorIds() = authorIds.filterNot { it == "unknown" }
