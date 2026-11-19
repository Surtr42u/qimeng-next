package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.AuthorProfileViewModel
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.ui.component.*
import media.qimeng.next.ui.theme.QimengDimens

@Composable
internal fun AuthorCollectionScreen(library: LibraryViewModel, id: String, profiles: AuthorProfileViewModel = viewModel()) {
    val actor by library.author.collectAsStateWithLifecycle()
    val profile by profiles.profile.collectAsStateWithLifecycle()
    val busy by library.busy.collectAsStateWithLifecycle()
    val follow by library.authorFollow.collectAsStateWithLifecycle()
    val author = actor?.takeIf { it.id == id }
    val document = profile.data?.takeIf { it.authorId == id }
    var updates by rememberSaveable(id) { mutableStateOf(false) }
    var chinese by rememberSaveable(id) { mutableStateOf(true) }
    val panes = rememberSaveableStateHolder()
    LaunchedEffect(id) {
        if (author == null) library.openAuthor(id)
        profiles.load(id)
    }
    val details = { library.navigate("profile:$id") }
    val header: @Composable () -> Unit = {
        Column {
            CreatorProfileCard(
                id = id, author = author, profile = profile, chinese = chinese, onLanguage = { chinese = it },
                followBusy = follow.data == id && follow.loading, followError = follow.error.takeIf { follow.data == id },
                enabled = !busy, onFollow = { library.followAuthor(id) }, onDetails = details, onRetry = { profiles.load(id) },
            )
            QimengChipRow(listOf(QimengPill("本地作品", !updates), QimengPill("作者新作", updates)), { updates = it == 1 },
                Modifier.padding(horizontal = QimengDimens.ScreenPaddingHorizontal, vertical = QimengDimens.SpaceM))
        }
    }
    Column(Modifier.fillMaxSize()) {
        QimengTopBar(author?.name ?: "作者", onBack = library::back)
        if (updates || author == null) {
            panes.SaveableStateProvider(if (updates) "updates" else "author-loading") {
                LazyColumn(
                    modifier = Modifier.fillMaxSize().padding(top = QimengDimens.SpaceM),
                    contentPadding = PaddingValues(start = QimengDimens.SpaceM, end = QimengDimens.SpaceM, bottom = QimengDimens.ListBottomContentPadding),
                ) {
                    item(key = "author-heading") { header() }
                    item {
                        Box(Modifier.fillMaxWidth().padding(horizontal = QimengDimens.ScreenPaddingHorizontal)) {
                            if (updates) AuthorUpdatesSection(id, document, settings = false, details = details)
                            else if (busy) LinearProgressIndicator(Modifier.fillMaxWidth())
                            else TextButton(onClick = { library.openAuthor(id) }) { Text("读取作者 · 重试") }
                        }
                    }
                }
            }
        } else {
            panes.SaveableStateProvider("local") {
                MediaBrowser(library, "author:$id", author = id, albumTitle = author.name, authorKind = author.kind, leading = header)
            }
        }
    }
}
