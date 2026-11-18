package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.AuthorProfileViewModel
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.*

@Composable
internal fun AuthorCollectionScreen(library: LibraryViewModel, id: String, profiles: AuthorProfileViewModel = viewModel()) {
    val actor by library.author.collectAsStateWithLifecycle()
    val profile by profiles.profile.collectAsStateWithLifecycle()
    val busy by library.busy.collectAsStateWithLifecycle()
    val author = actor?.takeIf { it.id == id }
    val document = profile.data?.takeIf { it.authorId == id }
    var updates by rememberSaveable(id) { mutableStateOf(false) }
    var translated by rememberSaveable(id) { mutableStateOf(false) }
    LaunchedEffect(id) {
        if (author == null) library.openAuthor(id)
        profiles.load(id)
    }
    val details = { library.navigate("profile:$id") }
    val header: @Composable () -> Unit = {
        Column {
            QimengSettingsCard(Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) {
                Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        AuthorAvatar(id, author?.name.orEmpty(), 64.dp)
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(author?.name.orEmpty(), style = MaterialTheme.typography.titleLarge)
                            Text("${author?.fileCount ?: "—"} 个本地文件", style = MaterialTheme.typography.bodySmall)
                        }
                        if (author != null) OutlinedButton(onClick = library::followAuthor, enabled = !busy) { Text(if (author.followed) "已关注" else "关注") }
                    }
                    val x = document?.blocks?.find { it.block == "x" }?.values
                    val biography = if (translated && !x?.biographyZh.isNullOrBlank()) x?.biographyZh else x?.biographyOriginal
                    if (!biography.isNullOrBlank()) Text(biography, maxLines = 4, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium)
                    Row {
                        PublicAuthorLink("前往 X ↗", x?.profileUrl)
                        TextButton(onClick = details) { Text("详细资料") }
                    }
                    if (!x?.biographyZh.isNullOrBlank()) Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                        QimengSegPill("中文", translated, { translated = true })
                        QimengSegPill("原文", !translated, { translated = false })
                    }
                    profile.error?.let { ErrorNotice(it) { profiles.load(id) } }
                }
            }
            QimengChipRow(listOf(QimengPill("本地作品", !updates), QimengPill("作者新作", updates)), { updates = it == 1 },
                Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
        }
    }
    Column(Modifier.fillMaxSize()) {
        QimengTopBar(author?.name ?: "作者", onBack = library::back)
        if (updates) LazyColumn(contentPadding = PaddingValues(bottom = 24.dp)) {
            item { header() }
            item { Box(Modifier.padding(horizontal = 16.dp)) { AuthorUpdatesSection(id, document, settings = false, details = details) } }
        } else if (author != null) MediaBrowser(library, "author:$id", author = id, albumTitle = author.name, authorKind = author.kind, leading = header)
        else TextButton(onClick = { library.openAuthor(id) }, enabled = !busy) { Text("读取作者 · 重试") }
    }
}
