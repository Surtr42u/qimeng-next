package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import media.qimeng.next.LoadState
import media.qimeng.next.model.Author
import media.qimeng.next.model.ProfileDocument
import media.qimeng.next.ui.component.QimengSegPill
import media.qimeng.next.ui.component.QimengSettingsCard
import media.qimeng.next.ui.theme.QimengDimens

/** Author header layout from the installed CreatorProfileCard, using Next profile and follow state. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun CreatorProfileCard(
    id: String,
    author: Author?,
    profile: LoadState<ProfileDocument>,
    chinese: Boolean,
    onLanguage: (Boolean) -> Unit,
    followBusy: Boolean,
    followError: String?,
    enabled: Boolean,
    onFollow: () -> Unit,
    onDetails: () -> Unit,
    onRetry: () -> Unit,
) {
    val document = profile.data?.takeIf { it.authorId == id }
    val x = document?.blocks?.find { it.block == "x" }?.values
    val translation = x?.biographyZh?.takeIf(String::isNotBlank)
    val biography = if (chinese && translation != null) translation else x?.biographyOriginal
    val name = author?.name ?: "作者"
    Column(
        Modifier.fillMaxWidth().padding(horizontal = QimengDimens.ScreenPaddingHorizontal, vertical = QimengDimens.SpaceM),
    ) {
        CreatorSection(Modifier.testTag("author-profile-card")) {
            val follow: @Composable () -> Unit = {
                if (author != null) TextButton(onClick = onFollow, enabled = enabled && !followBusy) {
                    Text(if (followBusy) "处理中…" else if (author.followed) "已关注" else "关注")
                }
            }
            BoxWithConstraints(Modifier.fillMaxWidth()) {
                val inlineFollow = maxWidth >= 96.dp + (132 * LocalDensity.current.fontScale).dp
                Column(Modifier.fillMaxWidth()) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                        AuthorAvatar(id, name, 64.dp)
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(name, Modifier.testTag("author-profile-name"), style = MaterialTheme.typography.titleLarge)
                            Text("${author?.fileCount ?: "—"} 个本地文件", style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        if (inlineFollow) follow()
                    }
                    if (!inlineFollow && author != null) Box(Modifier.align(Alignment.End)) { follow() }
                }
            }
            if (!biography.isNullOrBlank()) Text(biography, style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (profile.loading && document == null) Text("正在读取作者资料…", style = MaterialTheme.typography.bodySmall)
            FlowRow(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                FlowRow(Modifier.align(Alignment.CenterVertically), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    PublicAuthorLink("前往 X ↗", x?.profileUrl)
                    TextButton(onClick = onDetails) { Text("详细资料") }
                }
                if (translation != null) Box(Modifier.align(Alignment.CenterVertically)) {
                    BiographyLanguage(chinese, onLanguage)
                }
            }
            profile.error?.let {
                Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                TextButton(onClick = onRetry, enabled = !profile.loading) { Text("重新读取") }
            }
            followError?.let {
                Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                TextButton(onClick = onFollow, enabled = enabled && !followBusy) { Text("重试关注") }
            }
        }
    }
}

@Composable
internal fun CreatorSection(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    QimengSettingsCard(modifier) {
        Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp), content = content)
    }
}

@Composable
internal fun BiographyLanguage(chinese: Boolean, onLanguage: (Boolean) -> Unit, enabled: Boolean = true) {
    Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        QimengSegPill("中文", chinese, onClick = { onLanguage(true) }, enabled = enabled)
        QimengSegPill("原文", !chinese, onClick = { onLanguage(false) }, enabled = enabled)
    }
}
