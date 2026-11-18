package media.qimeng.next.screen

import android.net.Uri
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.AuthorUpdatesViewModel
import media.qimeng.next.model.ProfileDocument
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.QimengCapsuleTextField
import media.qimeng.next.ui.component.QimengSettingsCard
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
                                  model: AuthorUpdatesViewModel = viewModel()) {
    val target by model.session.collectAsStateWithLifecycle()
    val state by model.state.collectAsStateWithLifecycle()
    LaunchedEffect(target, id) { target?.let { model.bind(it, id) } }
    val feed = state.data?.takeIf { it.authorId == id }
    var expanded by rememberSaveable(id) { mutableStateOf(false) }
    var editRevision by rememberSaveable(id) { mutableStateOf<Long?>(null) }
    var draftUrl by rememberSaveable(id) { mutableStateOf("") }
    var enabled by rememberSaveable(id) { mutableStateOf(false) }
    val accounts = profile?.blocks?.find { it.block == "accounts" }?.values
    QimengSettingsCard { Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(if (settings) "作品发布订阅" else "作者新作", style = MaterialTheme.typography.titleMedium)
        if (state.loading && feed == null) LinearProgressIndicator(Modifier.fillMaxWidth())
        if (feed != null) {
            if (feed.enabled) {
                Text(feed.checkedAt?.let { "上次检查 $it" } ?: "尚未检查", style = MaterialTheme.typography.bodySmall)
                TextButton(onClick = { target?.let { model.check(it, feed) } }, enabled = !state.loading) { Text(if (state.loading) "正在检查…" else "检查动态") }
                if (!settings) {
                    if (feed.items.isEmpty()) Text("尚未取得新作记录", style = MaterialTheme.typography.bodyMedium)
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
            if (settings) TextButton(onClick = { draftUrl = feed.url; enabled = feed.enabled; editRevision = feed.revision }, enabled = !state.loading) {
                Text(if (feed.enabled) "编辑订阅" else "设置订阅")
            } else TextButton(onClick = details) { Text("发布来源与详细资料") }
        }
        if (editRevision == null) state.error?.let { ErrorNotice(it) { target?.let { owner -> model.bind(owner, id, refresh = true) } } }
    } }
    val revision = editRevision ?: return
    if (feed == null) return
    val key = rememberSaveable(id, revision, draftUrl, enabled) { UUID.randomUUID().toString() }
    AlertDialog(onDismissRequest = { if (!state.loading) editRevision = null }, title = { Text("作品发布订阅") },
        text = { Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row { Checkbox(enabled, { enabled = it }, enabled = !state.loading); Text("跟踪作者新作") }
            QimengCapsuleTextField(draftUrl, { draftUrl = it }, placeholder = "公开 HTTPS RSS / Atom 地址", singleLine = true, enabled = !state.loading)
            if (enabled && accounts?.identityStatus != "verified") Text("请先在其他账号资料中确认作者身份。", style = MaterialTheme.typography.bodySmall)
            state.error?.let { ErrorNotice(it) }
        } }, confirmButton = { TextButton(onClick = {
            target?.let { model.save(it, feed.copy(revision = revision), enabled, draftUrl.trim(), key) { editRevision = null } }
        }, enabled = !state.loading && (!enabled || (accounts?.identityStatus == "verified" && draftUrl.isNotBlank()))) { Text("保存") } },
        dismissButton = { TextButton(onClick = { editRevision = null }, enabled = !state.loading) { Text("取消") } })
}
