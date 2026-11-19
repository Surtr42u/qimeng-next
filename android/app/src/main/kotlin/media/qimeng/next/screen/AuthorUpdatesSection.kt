package media.qimeng.next.screen

import android.net.Uri
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.AuthorUpdatesViewModel
import media.qimeng.next.model.ProfileDocument
import media.qimeng.next.ui.ErrorNotice
import java.util.UUID

@Composable
internal fun PublicAuthorLink(label: String, address: String?) {
    val uri = address?.let(Uri::parse) ?: return
    if (uri.scheme !in setOf("http", "https") || uri.host.isNullOrEmpty() || uri.userInfo != null) return
    val browser = LocalUriHandler.current
    var error by remember(address) { mutableStateOf(false) }
    TextButton(onClick = {
        try { browser.openUri(uri.toString()); error = false } catch (_: IllegalArgumentException) { error = true }
    }) { Text(label) }
    if (error) Text("没有可用的浏览器", style = MaterialTheme.typography.bodySmall)
}

@Composable
internal fun AuthorUpdatesSection(id: String, profile: ProfileDocument?, settings: Boolean, details: () -> Unit,
                                  authorName: String = "",
                                  model: AuthorUpdatesViewModel = viewModel()) {
    val target by model.session.collectAsStateWithLifecycle()
    val state by model.state.collectAsStateWithLifecycle()
    LaunchedEffect(target, id) { target?.let { model.bind(it, id) } }
    val feed = state.data?.takeIf { it.authorId == id }
    var expanded by rememberSaveable(id) { mutableStateOf(false) }
    var editRevision by rememberSaveable(id, target?.instanceId) { mutableStateOf<Long?>(null) }
    var draftUrl by rememberSaveable(id) { mutableStateOf("") }
    var enabled by rememberSaveable(id) { mutableStateOf(false) }
    var initialUrl by rememberSaveable(id) { mutableStateOf("") }
    var initialEnabled by rememberSaveable(id) { mutableStateOf(false) }
    val accounts = profile?.blocks?.find { it.block == "accounts" }?.values
    val editable = profile?.editable == true
    CreatorSection {
        Row(Modifier.fillMaxWidth()) {
            Text(if (settings) "作品订阅" else "作者新作", Modifier.weight(1f).align(Alignment.CenterVertically), style = MaterialTheme.typography.titleSmall)
            if (settings && editable && feed != null) TextButton(onClick = {
                initialUrl = feed.url; draftUrl = feed.url
                initialEnabled = feed.enabled; enabled = feed.enabled
                editRevision = feed.revision
            }, enabled = !state.loading) { Text("编辑作品订阅") }
        }
        if (state.loading && feed == null) LinearProgressIndicator(Modifier.fillMaxWidth())
        if (feed != null) {
            if (settings) {
                Text(if (feed.url.isBlank()) "尚未配置作品发布订阅" else "已配置作品发布来源", style = MaterialTheme.typography.bodyMedium)
                Text("订阅检查 · ${if (feed.enabled) "已开启" else "已关闭"}", style = MaterialTheme.typography.bodySmall)
                PublicAuthorLink("查看订阅来源 ↗", feed.url)
            }
            if (feed.enabled) {
                Text(feed.checkedAt?.let { "上次检查 $it" } ?: "尚未检查", style = MaterialTheme.typography.bodySmall)
                if (editable) TextButton(onClick = { target?.let { model.check(it, feed) } }, enabled = !state.loading) {
                    Text(if (state.loading) "正在检查…" else "检查动态")
                }
                if (!settings) {
                    if (feed.items.isEmpty() && state.error == null && feed.checkedAt != null) Text("暂无新作记录", style = MaterialTheme.typography.bodyMedium)
                    (if (expanded) feed.items else feed.items.take(3)).forEach { entry ->
                        HorizontalDivider()
                        PublicAuthorLink(entry.title.ifBlank { "查看原帖" }, entry.url)
                        entry.publishedAt?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
                        Text("外部内容 · 在浏览器打开", style = MaterialTheme.typography.labelSmall)
                    }
                    if (feed.items.size > 3) TextButton(onClick = { expanded = !expanded }) { Text(if (expanded) "收起动态" else "查看全部 ${feed.items.size} 条记录") }
                }
            } else if (!settings) {
                Text("尚未接入可用的作品发布订阅，可前往作者发布页查看新作。", style = MaterialTheme.typography.bodyMedium)
                PublicAuthorLink("查看 X 发布 ↗", profile?.blocks?.find { it.block == "x" }?.values?.profileUrl)
                accounts?.accounts.orEmpty().forEach { account ->
                    if (account.publishingUrl.isNotEmpty()) PublicAuthorLink("${account.platform} 发布页 ↗", account.publishingUrl)
                }
            }
            if (!settings) TextButton(onClick = details) { Text("发布来源与详细资料") }
        }
        if (editRevision == null) state.error?.let { ErrorNotice(it) { target?.let { owner -> model.bind(owner, id, refresh = true) } } }
    }
    val revision = editRevision ?: return
    if (feed == null || !editable) return
    val dirty = draftUrl.trim() != initialUrl || enabled != initialEnabled
    val stale = feed.revision != revision
    val key = rememberSaveable(id, revision, draftUrl, enabled) { UUID.randomUUID().toString() }
    CreatorProfileEditorLayout(
        title = "作品订阅", authorName = authorName, busy = state.loading, dirty = dirty,
        canSave = dirty && !stale && (!enabled || (accounts?.identityStatus == "verified" && draftUrl.isNotBlank())),
        error = if (stale) "订阅资料已更新。请关闭草稿并刷新后继续编辑。" else state.error,
        onClose = { editRevision = null },
        onSave = { target?.let { model.save(it, feed.copy(revision = revision), enabled, draftUrl.trim(), key) { editRevision = null } } },
        saveLabel = "保存订阅",
    ) {
        CreatorSection {
            OutlinedTextField(draftUrl, { draftUrl = it }, modifier = Modifier.fillMaxWidth(),
                label = { Text("作品发布订阅地址（RSS / Atom）") }, singleLine = true, enabled = !state.loading)
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(enabled, { enabled = it }, enabled = !state.loading)
                Text("跟踪作者新作")
            }
            if (enabled && accounts?.identityStatus != "verified") Text("请先在账号来源中确认作者身份。", style = MaterialTheme.typography.bodySmall)
        }
    }
}
