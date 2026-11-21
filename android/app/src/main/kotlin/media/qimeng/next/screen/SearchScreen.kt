package media.qimeng.next.screen

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.FocusState
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.SearchViewModel
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.*
import media.qimeng.next.ui.icon.BackIcon
import media.qimeng.next.ui.icon.SearchIcon
import media.qimeng.next.ui.icon.ClearIcon

/** Existing SearchScreen top row, entry words, suggestion rows and two result filters; new state/API owner. */
@Composable
fun SearchScreen(library: LibraryViewModel, initialTag: String? = null, model: SearchViewModel = viewModel()) {
    val query by model.query.collectAsStateWithLifecycle()
    val submitted by model.submitted.collectAsStateWithLifecycle()
    val phase by model.phase.collectAsStateWithLifecycle()
    val history by model.history.collectAsStateWithLifecycle()
    val recommendWords by model.recommendWords.collectAsStateWithLifecycle()
    val words by model.words.collectAsStateWithLifecycle()
    val error by model.error.collectAsStateWithLifecycle()
    val controls = rememberBrowserControls("search")
    val keyboard = LocalSoftwareKeyboardController.current
    val focus = LocalFocusManager.current
    val field = remember { FocusRequester() }
    // 搜索框文本与选区同持（TextFieldValue 重载）：String 重载的 selection 锁在实现内部，
    // 结果态提交失焦后重新聚焦时折叠光标停在 0，退格删不到字。外部 query 变化（deep link、
    // 提交时 trim、返回清词）才重建文本并把选区放到末尾；用户输入路径保留 IME 组合区与选区。
    var fieldValue by remember { mutableStateOf(TextFieldValue(query, TextRange(query.length))) }
    LaunchedEffect(query) {
        if (fieldValue.text != query) fieldValue = TextFieldValue(query, TextRange(query.length))
    }
    LaunchedEffect(initialTag) { model.open(initialTag) }
    LaunchedEffect(phase) { if (phase != "result") library.cancelBrowse("search") }
    DisposableEffect(library) {
        onDispose {
            if (!library.route.value.startsWith("asset:")) library.cancelBrowse("search")
        }
    }
    val back = {
        focus.clearFocus()
        keyboard?.hide()
        controls.type.value = null
        controls.partition.value = null
        library.clearSearchResults()
        if (!model.back()) library.back()
    }
    val submit: (String) -> Unit = { word ->
        if (word.isNotBlank()) {
            controls.type.value = null
            controls.partition.value = null
            focus.clearFocus()
            keyboard?.hide()
            model.submit(word)
        }
    }
    val onFieldTap by rememberUpdatedState {
        model.enterEditing()
        // 结果态点击被 interceptSearchResultTap 在 Initial 阶段吃掉，输入框收不到这次手势；
        // 显式把折叠光标放到已有文本末尾（只移光标，不 select-all），退格即可删上一轮问句。
        fieldValue = fieldValue.copy(selection = TextRange(fieldValue.text.length))
        field.requestFocus()
        keyboard?.show()
    }
    // 任意路径获得焦点（建议态点框、入口态点框、结果态点回、deep link 后回来）都走这里：
    // 仅当选择折叠在最左端且文本非空时把光标移到末尾；用户正在输入/框选时焦点不变，不触发。
    val onFieldFocusChanged: (FocusState) -> Unit = { state ->
        if (state.isFocused) {
            searchFieldFocusCaret(fieldValue.text.length, fieldValue.selection.start, fieldValue.selection.end)
                ?.let { caret -> fieldValue = fieldValue.copy(selection = TextRange(caret)) }
        }
    }
    BackHandler(onBack = back)
    Column(Modifier.fillMaxSize()) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = back) { Icon(BackIcon, "返回") }
            Box(Modifier.weight(1f).interceptSearchResultTap(phase == "result") { onFieldTap() }) {
                QimengCapsuleTextField(fieldValue, { next: TextFieldValue ->
                    // 只有文本变化才经 ViewModel 落账（model.query 仍是真相）；纯光标/选区/IME
                    // 组合变化只更新本地 TextFieldValue，避免移动光标也重发一次建议查询。
                    if (next.text != fieldValue.text) model.edit(next.text)
                    fieldValue = next
                }, placeholder = "搜索文件、出处、角色、COS作者…", singleLine = true,
                    leadingIcon = { Icon(SearchIcon, null) }, keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                    keyboardActions = KeyboardActions(onSearch = { submit(query) }),
                    modifier = Modifier.fillMaxWidth().focusRequester(field).onFocusChanged(onFieldFocusChanged).testTag("search-input"))
            }
            TextButton(onClick = { submit(query) }) { Text("搜索", style = MaterialTheme.typography.labelLarge.copy(fontWeight = FontWeight.Normal)) }
        }
        error?.let { ErrorNotice(it) }
        when (phase) {
            "result" -> {
                Column(Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                    val types = listOf(null to "综合", "video" to "视频", "animated_image" to "动图", "image" to "图片")
                    QimengChipRow(types.map { QimengPill(it.second, it.first == controls.type.value) }, { controls.type.value = types[it].first }, Modifier.padding(horizontal = 16.dp))
                    Spacer(Modifier.height(4.dp))
                    val partitions = listOf(null to "全部", "regular" to "常规", "cos" to "COS")
                    QimengChipRow(partitions.map { QimengPill(it.second, it.first == controls.partition.value) }, { controls.partition.value = partitions[it].first }, Modifier.padding(horizontal = 16.dp))
                }
                MediaBrowser(library, "search", controls = controls, toolbar = false, textOverride = submitted, caption = true)
            }
            "suggest" -> {
                words.error?.let { ErrorNotice(it, model::refreshWords) }
                LazyColumn(Modifier.fillMaxSize()) {
                    items(words.data.orEmpty(), key = { "${it.type}:${it.id}" }) { word ->
                        Row(Modifier.fillMaxWidth().clickable { submit(word.name) }.padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                            Icon(SearchIcon, "候选", tint = MaterialTheme.colorScheme.onSurfaceVariant)
                            Spacer(Modifier.padding(start = 8.dp))
                            Text(word.name, style = MaterialTheme.typography.bodyMedium)
                            Spacer(Modifier.weight(1f))
                            Text(mapOf("source" to "出处", "character" to "角色", "cosAuthor" to "COS作者", "cosWork" to "COS作品", "author" to "作者")[word.type] ?: word.type,
                                style = MaterialTheme.typography.labelSmall.copy(fontSize = 10.sp, fontWeight = FontWeight.Normal), color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
            }
            else -> LazyColumn(Modifier.fillMaxSize().padding(horizontal = 16.dp)) {
                item {
                    SearchSectionHeader("推荐搜索")
                    recommendWords.error?.let { ErrorNotice(it, model::refreshWords) }
                    QimengWordPillFlow(recommendWords.data.orEmpty().map { QimengPill(it.name) }, { submit(recommendWords.data.orEmpty()[it].name) }, Modifier.padding(bottom = 12.dp))
                }
                if (history.isNotEmpty()) item {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        SearchSectionHeader("搜索历史"); Spacer(Modifier.weight(1f))
                        IconButton(onClick = model::clearHistory) { Icon(ClearIcon, "清除历史") }
                    }
                    QimengWordPillFlow(history.map { QimengPill(it) }, { submit(history[it]) }, Modifier.padding(bottom = 12.dp))
                }
            }
        }
    }
}

@Composable
private fun SearchSectionHeader(text: String) { Text(text, style = MaterialTheme.typography.titleSmall.copy(fontSize = 13.sp),
    color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(vertical = 8.dp)) }

/**
 * 搜索框重新获得焦点时的折叠光标落点（返回 null=保持当前选区）。
 *
 * 结果态提交会 clearFocus，再点回搜索栏时选择常从 0 开始：只有「文本非空 + 选择折叠在 0」
 * 才把光标移到末尾，让退格直接删掉上一轮问句。用户主动点在中间、框选文本或光标已在末尾时
 * 不动；输入过程不产生焦点事件，所以不会干扰打字与 IME。
 */
internal fun searchFieldFocusCaret(textLength: Int, selectionStart: Int, selectionEnd: Int): Int? =
    if (textLength > 0 && selectionStart == 0 && selectionEnd == 0) textLength else null

/** Retained pure UI gesture: result-field tap returns to suggestions before the text field handles it. */
private fun Modifier.interceptSearchResultTap(enabled: Boolean, onTap: () -> Unit): Modifier = if (!enabled) this else pointerInput(Unit) {
    awaitEachGesture {
        awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
        var pressed = true
        while (pressed) {
            val event = awaitPointerEvent(PointerEventPass.Initial)
            event.changes.forEach { it.consume() }
            pressed = event.changes.any { it.pressed }
        }
        onTap()
    }
}
