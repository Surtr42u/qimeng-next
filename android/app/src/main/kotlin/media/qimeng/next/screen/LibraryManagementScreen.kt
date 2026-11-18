package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.CancellationException
import media.qimeng.next.LibraryManagementViewModel
import media.qimeng.next.libraryError
import media.qimeng.next.model.Library
import media.qimeng.next.model.Session
import media.qimeng.next.runtime.LocalFolderPage
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.*
import media.qimeng.next.ui.theme.qimengFilledButtonColors
import java.util.UUID

@Composable
internal fun LibraryManagementScreen(onBack: () -> Unit, model: LibraryManagementViewModel = viewModel()) {
    val session by model.session.collectAsStateWithLifecycle()
    val target = session ?: return
    val list by model.libraries.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val notice by model.notice.collectAsStateWithLifecycle()
    val scans by model.localScans.collectAsStateWithLifecycle()
    val access by model.fileAccess.collectAsStateWithLifecycle()
    val local = model.owned(target)
    var deleting by remember(target) { mutableStateOf<Library?>(null) }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { model.refreshAccess(); model.refresh() }
    val activeScan = list.items.any { it.scanState == "scanning" }
    LaunchedEffect(activeScan, scans.requests) {
        if (local && scans.requests.any { it.instanceId == target.instanceId && it.state == "submitted" }) model.refresh()
        if (activeScan) while (true) { kotlinx.coroutines.delay(4_000); model.refresh() }
    }
    Column(Modifier.fillMaxSize()) {
        QimengTopBar("库管理", onBack = onBack)
        LazyColumn(Modifier.fillMaxSize().imePadding(), contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp)) {
            item { QimengPageIntro("管理已登记的媒体目录，也可以注册新目录后发起扫描。") }
            list.error?.let { item { ErrorNotice(it) { model.refresh(next = list.cursor != null) } } }
            notice?.let { item { Text(it, style = MaterialTheme.typography.bodySmall) } }
            item { Row(Modifier.fillMaxWidth().padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text("媒体库", Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
                list.totalMatched?.let { Text("$it 个库", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            } }
            if (!list.loaded && list.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
            if (list.loaded && list.items.isEmpty()) item { Text("还没有库，先在下方注册一个媒体文件夹。", style = MaterialTheme.typography.bodyMedium) }
            items(list.items, key = { it.id }) { library ->
                val queued = if (local) scans.requests.firstOrNull { it.instanceId == target.instanceId && it.libraryId == library.id && it.state != "submitted" } else null
                LibraryRow(target, library, queued?.message, busy, model, { deleting = library })
            }
            if (list.cursor != null) item { TextButton(onClick = { model.refresh(next = true) }, enabled = !list.loading) { Text("更多媒体库") } }
            item { RegisterLibrary(target, model, busy, local, access.granted) }
        }
    }
    deleting?.let { library ->
        AlertDialog(onDismissRequest = { deleting = null }, title = { Text("删除库「${library.name}」？") },
            text = { Text("该库 ${library.fileCount} 个文件的索引将被清除，磁盘文件不受影响。") },
            confirmButton = { TextButton(onClick = { model.delete(library); deleting = null }, enabled = !busy) { Text("删除") } },
            dismissButton = { TextButton(onClick = { deleting = null }) { Text("取消") } })
    }
}

@Composable
private fun LibraryRow(target: Session, library: Library, queued: String?, busy: Boolean, model: LibraryManagementViewModel, remove: () -> Unit) {
    var attempt by remember(target, library.id) { mutableIntStateOf(0) }
    var root by remember(target, library.id) { mutableStateOf<String?>(null) }
    var error by remember(target, library.id) { mutableStateOf<String?>(null) }
    LaunchedEffect(target, library.id, attempt) {
        try { root = model.location(target, library.id); error = null }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) { error = libraryError(failure) }
    }
    QimengSettingsCard(Modifier.fillMaxWidth()) {
        Column(Modifier.fillMaxWidth().padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(library.name, Modifier.weight(1f), style = MaterialTheme.typography.titleSmall)
                Surface(shape = androidx.compose.foundation.shape.CircleShape, color = MaterialTheme.colorScheme.surfaceVariant) {
                    Text(if (library.kind == "cos") "COS" else "常规", Modifier.padding(horizontal = 10.dp, vertical = 2.dp), style = MaterialTheme.typography.labelMedium)
                }
            }
            error?.let { ErrorNotice(it) { attempt++ } }
            root?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(queued ?: when (library.scanState) { "idle" -> "空闲"; "scanning" -> "扫描中"; "error" -> "异常"; else -> library.scanState },
                    Modifier.weight(1f), style = MaterialTheme.typography.bodySmall,
                    color = when (library.scanState) { "scanning" -> MaterialTheme.colorScheme.primary; "error" -> MaterialTheme.colorScheme.error; else -> MaterialTheme.colorScheme.onSurfaceVariant })
                Text("${library.fileCount} 个文件", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Switch(library.enabled, { model.enabled(library, it) }, enabled = !busy)
                Spacer(Modifier.weight(1f))
                TextButton(onClick = { model.rescan(library) }, enabled = !busy && library.scanState != "scanning") { Text("重新扫描") }
                TextButton(onClick = remove, enabled = !busy) { Text("删除") }
            }
        }
    }
}

@Composable
private fun RegisterLibrary(target: Session, model: LibraryManagementViewModel, busy: Boolean, local: Boolean, access: Boolean) {
    var name by rememberSaveable(target.instanceId) { mutableStateOf("") }
    var path by rememberSaveable(target.instanceId) { mutableStateOf("") }
    var cos by rememberSaveable(target.instanceId) { mutableStateOf(false) }
    var browsing by rememberSaveable(target.instanceId) { mutableStateOf(false) }
    val key = rememberSaveable(target.instanceId, name, path, cos) { UUID.randomUUID().toString() }
    QimengSettingsCard(Modifier.fillMaxWidth()) {
        Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("注册新库", style = MaterialTheme.typography.titleMedium)
            Text("把服务端可访问的磁盘文件夹登记为媒体库，注册后自动扫描入库", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text("库名称", style = MaterialTheme.typography.labelLarge)
            QimengCapsuleTextField(name, { name = it }, placeholder = "例如：1 图集", singleLine = true, enabled = !busy)
            Text("根路径（服务端可访问的绝对路径）", style = MaterialTheme.typography.labelLarge)
            QimengCapsuleTextField(path, { path = it }, placeholder = if (local) "本机媒体目录" else "服务端媒体目录", singleLine = true, enabled = !busy)
            if (local) TextButton(onClick = { if (access) browsing = true else model.requestFileAccess() }, enabled = !busy) {
                Text(if (access) "选择本机文件夹" else "去系统设置授权")
            }
            Text("不能位于服务端数据目录内；COS 库按「作者/作品/文件」目录结构组织", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text("库类型", style = MaterialTheme.typography.labelLarge)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                QimengSegPill("常规", !cos, { if (!busy) cos = false })
                QimengSegPill("COS 作者库", cos, { if (!busy) cos = true })
            }
            Button(onClick = { model.register(name.trim(), path, cos, key) { name = ""; path = ""; cos = false } },
                enabled = !busy && name.isNotBlank() && path.isNotBlank(), colors = qimengFilledButtonColors(), modifier = Modifier.fillMaxWidth()) { Text("注册并扫描") }
        }
    }
    if (browsing && local && access) LocalFolderPicker(model, { browsing = false }) { path = it; browsing = false }
}

@Composable
private fun LocalFolderPicker(model: LibraryManagementViewModel, dismiss: () -> Unit, choose: (String) -> Unit) {
    var path by rememberSaveable { mutableStateOf<String?>(null) }
    var cursor by remember(path) { mutableStateOf<String?>(null) }
    var attempt by remember { mutableIntStateOf(0) }
    var page by remember(path) { mutableStateOf<LocalFolderPage?>(null) }
    var error by remember(path) { mutableStateOf<String?>(null) }
    var loading by remember(path) { mutableStateOf(true) }
    LaunchedEffect(path, cursor, attempt) {
        loading = true; error = null
        try {
            val next = model.folders(path, cursor)
            page = if (cursor == null) next else next.copy(entries = (page?.entries.orEmpty() + next.entries).distinctBy { it.path })
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) { error = libraryError(failure) }
        finally { loading = false }
    }
    AlertDialog(onDismissRequest = dismiss, title = { Text("选择本机文件夹") }, text = {
        Column {
            page?.let { current ->
                Text(current.currentPath, style = MaterialTheme.typography.bodySmall)
                current.parentPath?.let { parent -> TextButton(onClick = { path = parent }) { Text("上一级") } }
            }
            error?.let { ErrorNotice(it) { attempt++ } }
            LazyColumn(Modifier.heightIn(max = 320.dp)) {
                items(page?.entries.orEmpty(), key = { it.path }) { child -> TextButton(onClick = { path = child.path }) { Text(child.name) } }
                page?.nextCursor?.let { next -> item { TextButton(onClick = { cursor = next }, enabled = !loading) { Text("更多目录") } } }
            }
        }
    }, confirmButton = { TextButton(onClick = { page?.let { choose(it.currentPath) } }, enabled = page != null && !loading) { Text("选择当前目录") } },
        dismissButton = { TextButton(onClick = dismiss) { Text("取消") } })
}
