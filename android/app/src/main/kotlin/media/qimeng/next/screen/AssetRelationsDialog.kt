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
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.ManagementViewModel
import media.qimeng.next.model.Asset
import media.qimeng.next.ui.ErrorNotice

@Composable
internal fun AssetRelationsDialog(asset: Asset, library: LibraryViewModel, management: ManagementViewModel = viewModel(), close: () -> Unit) {
    val revision = rememberSaveable(asset.id) { asset.revision }
    var kind by rememberSaveable { mutableStateOf("sources") }
    var sources by rememberSaveable(asset.id) { mutableStateOf(asset.sourceIds) }
    var characters by rememberSaveable(asset.id) { mutableStateOf(asset.characterIds) }
    val entries by management.entries.collectAsStateWithLifecycle()
    val busy by library.busy.collectAsStateWithLifecycle()
    val message by library.message.collectAsStateWithLifecycle()
    LaunchedEffect(kind) { management.load(kind) }
    AlertDialog(onDismissRequest = { if (!busy) close() }, title = { Text("关联原作与角色") }, text = {
        Column(Modifier.heightIn(max = 440.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(kind == "sources", { kind = "sources" }, label = { Text("出处 · ${sources.size}") })
                FilterChip(kind == "characters", { kind = "characters" }, label = { Text("角色 · ${characters.size}") })
            }
            if (entries.loading) LinearProgressIndicator(Modifier.fillMaxWidth())
            entries.error?.let { ErrorNotice(it) { management.load(kind) } }
            LazyColumn(Modifier.weight(1f, fill = false)) {
                items(entries.items, key = { it.id }) { entry ->
                    val selected = entry.id in if (kind == "sources") sources else characters
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(selected, { checked ->
                            if (kind == "sources") sources = if (checked) sources + entry.id else sources - entry.id
                            else characters = if (checked) characters + entry.id else characters - entry.id
                        })
                        Text(entry.name)
                    }
                }
                if (entries.cursor != null) item { TextButton(onClick = { management.load(kind, true) }, enabled = !entries.loading) { Text("加载更多") } }
            }
            message?.let { ErrorNotice(it) }
        }
    }, confirmButton = { TextButton(onClick = { library.saveRelations(asset.copy(revision = revision), sources, characters, close) }, enabled = !busy) { Text("保存") } },
        dismissButton = { TextButton(onClick = close, enabled = !busy) { Text("取消") } })
}
