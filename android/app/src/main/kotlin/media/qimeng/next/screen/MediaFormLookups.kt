package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import media.qimeng.next.ListState
import media.qimeng.next.LoadState
import media.qimeng.next.MediaFormLookupsViewModel
import media.qimeng.next.libraryError
import media.qimeng.next.model.Author
import media.qimeng.next.model.Session
import media.qimeng.next.model.acquisitionTerms
import media.qimeng.next.model.validFileStem
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.QimengCapsuleTextField
import media.qimeng.next.ui.component.QimengSegPill

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun AuthorLookup(
    target: Session,
    transport: Long,
    selectedIds: List<String>,
    enabled: Boolean,
    tag: String,
    onPick: (Author) -> Unit,
    onAddName: ((String) -> Unit)? = null,
    model: MediaFormLookupsViewModel = viewModel(),
) {
    var query by rememberSaveable(target.instanceId, target.baseUrl) { mutableStateOf("") }
    val term = query.trim()
    // 行只在连接/传输变化时重置：输入变化的重查沿用上一份结果，直到新页落地，胶囊不会每敲一字清空。
    var rows by remember(target, transport) { mutableStateOf(ListState<Author>(loading = true)) }
    var cursor by remember(target, transport, term) { mutableStateOf<String?>(null) }
    var request by remember(target, transport, term) { mutableIntStateOf(0) }
    val requestedCursor = cursor
    LaunchedEffect(target, transport, term, request, requestedCursor) {
        if (requestedCursor == null && term.isNotEmpty()) delay(250)
        val existing = if (requestedCursor == null) emptyList() else rows.items
        // 第一页重查作废旧游标：失败时不能拿上一词条的游标继续翻页。
        rows = rows.copy(loading = true, error = null, cursor = if (requestedCursor == null) null else rows.cursor)
        try {
            val page = model.authors(target, transport, term, requestedCursor)
            rows = ListState((existing + page.items.filter { it.id != "unknown" }).distinctBy(Author::id), page.nextCursor, loaded = true)
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) { rows = rows.copy(loading = false, error = libraryError(failure)) }
    }
    QimengCapsuleTextField(query, { query = it }, placeholder = "输入作者名（联想选择）", singleLine = true, enabled = enabled,
        modifier = Modifier.fillMaxWidth().testTag(tag))
    Column(Modifier.fillMaxWidth().heightIn(max = 240.dp).verticalScroll(rememberScrollState()),
        verticalArrangement = Arrangement.spacedBy(4.dp)) {
        // 只有还没有任何行时才用进度条占位；重查期间既有胶囊留在原处。
        if (rows.loading && rows.items.isEmpty()) LinearProgressIndicator(Modifier.fillMaxWidth())
        rows.error?.let { ErrorNotice(it) { request++ } }
        if (rows.loaded && !rows.loading && rows.error == null && rows.items.isEmpty()) Text("暂无匹配作者", style = MaterialTheme.typography.bodySmall)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            rows.items.forEach { author ->
                val namesake = rows.items.any { it.id != author.id && it.name == author.name }
                val label = if (namesake) "${author.name} · ${author.id.takeLast(6)}" else author.name
                QimengSegPill(label, author.id in selectedIds, { onPick(author) }, enabled = enabled,
                    modifier = Modifier.testTag("$tag:${author.id}"))
            }
        }
        rows.cursor?.let { next -> TextButton(onClick = { cursor = next }, enabled = !rows.loading && rows.error == null) { Text("更多作者") } }
    }
    if (onAddName != null && term.isNotEmpty() && term.codePointCount(0, term.length) <= 200 &&
        rows.loaded && !rows.loading && rows.error == null && rows.items.none { it.name == term }) {
        TextButton(onClick = { onAddName(term); query = "" }, enabled = enabled) { Text("添加作者名称「$term」") }
    }
}

@Composable
internal fun acquisitionOptions(target: Session, transport: Long, request: Int, model: MediaFormLookupsViewModel = viewModel()): LoadState<List<String>> {
    var state by remember(target, transport) { mutableStateOf(LoadState<List<String>>(loading = true)) }
    LaunchedEffect(target, transport, request) {
        // 重试沿用已读到的选项：只在成功或失败时落地新状态，胶囊行不会在请求期间消失。
        state = state.copy(loading = true, error = null)
        state = try { LoadState(data = model.sources(target, transport)) }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) { state.copy(loading = false, error = libraryError(failure)) }
    }
    return state
}

@Composable
internal fun AcquisitionChoiceRow(options: List<String>, selected: List<String>, enabled: Boolean, tag: String, toggle: (String) -> Unit) {
    if (options.isEmpty()) return
    LazyRow(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        items(options, key = { it }) { source ->
            QimengSegPill(source, source in selected, { toggle(source) }, enabled = enabled,
                modifier = Modifier.testTag("$tag:$source"))
        }
    }
}

internal fun toggleAcquisition(text: String, source: String): String {
    val selected = acquisitionTerms(text)
    return (if (source in selected) selected - source else selected + source).joinToString("\n")
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun FileNameHints(
    target: Session,
    transport: Long,
    libraryId: String,
    directory: String,
    stem: String,
    extension: String,
    numberClass: String,
    enabled: Boolean,
    choose: (String) -> Unit,
    model: MediaFormLookupsViewModel = viewModel(),
) {
    val term = stem.trim()
    if (libraryId.isBlank() || term.isEmpty() || term.toByteArray(Charsets.UTF_8).size > 200) return
    var choices by remember(target, transport, libraryId, directory, term, numberClass, extension) { mutableStateOf(LoadState<List<String>>()) }
    var request by remember { mutableIntStateOf(0) }
    LaunchedEffect(target, transport, libraryId, directory, term, numberClass, extension, request) {
        delay(300)
        choices = try { LoadState(data = model.fileStems(target, transport, libraryId, directory, term, numberClass)) }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) { LoadState(error = libraryError(failure)) }
    }
    choices.error?.let { ErrorNotice("文件名建议读取失败。\n$it") { request++ } }
    val values = choices.data.orEmpty().filter { it != term && validFileStem(it, extension) }
    if (values.isNotEmpty()) Surface(
        shape = MaterialTheme.shapes.medium,
        color = MaterialTheme.colorScheme.surfaceContainerHigh,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Column(Modifier.padding(10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("推荐文件名（点击采用）", style = MaterialTheme.typography.labelSmall)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                values.forEach { value -> QimengSegPill(value + extension, false, { choose(value) }, enabled = enabled) }
            }
        }
    }
}
