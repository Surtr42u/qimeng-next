package media.qimeng.next.screen

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.ThumbnailCachePage
import media.qimeng.next.ThumbnailCacheViewModel
import media.qimeng.next.model.ThumbnailCacheSelection
import media.qimeng.next.model.ThumbnailCoverage
import media.qimeng.next.model.ThumbnailPool
import media.qimeng.next.model.ThumbnailPoolUsage
import media.qimeng.next.model.ThumbnailPrefetch
import media.qimeng.next.model.ThumbnailPrefetchStatus
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.QimengPageIntro
import media.qimeng.next.ui.component.QimengSettingsCard
import media.qimeng.next.ui.component.QimengTopBar
import media.qimeng.next.ui.theme.QimengDimens

@Composable
internal fun ThumbnailCacheScreen(onBack: () -> Unit, model: ThumbnailCacheViewModel = viewModel()) {
    val page by model.state.collectAsStateWithLifecycle()
    val prefetch by model.prefetch.collectAsStateWithLifecycle()
    val lifecycleOwner = LocalLifecycleOwner.current
    LaunchedEffect(model, lifecycleOwner) {
        lifecycleOwner.repeatOnLifecycle(Lifecycle.State.STARTED) { model.observe() }
    }
    ThumbnailCacheContent(page, prefetch, model::clear, onBack)
}

@Composable
internal fun ThumbnailCacheContent(
    page: ThumbnailCachePage,
    prefetch: ThumbnailPrefetch?,
    onClear: (ThumbnailCacheSelection) -> Unit,
    onBack: () -> Unit,
) {
    var confirming by remember { mutableStateOf<ThumbnailCacheSelection?>(null) }
    Column(Modifier.fillMaxSize()) {
        QimengTopBar("缩略图缓存", onBack = onBack)
        Column(
            Modifier.fillMaxSize().navigationBarsPadding().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp),
            verticalArrangement = Arrangement.spacedBy(QimengDimens.SpaceL),
        ) {
            QimengPageIntro("按连接端分别管理缓存。清理缩略图缓存不会删除媒体原件。")
            page.error?.let { ErrorNotice(it) }
            CacheCard(ThumbnailPool.NAS, page.nas, prefetch?.takeIf { it.selection == page.nas?.selection }?.status,
                null, page.clearing, { confirming = it })
            CacheCard(ThumbnailPool.LOCAL, page.local, prefetch?.takeIf { it.selection == page.local?.selection }?.status,
                page.coverage?.takeIf { it.first == page.local?.selection }?.second, page.clearing, { confirming = it })
            Spacer(Modifier.height(24.dp))
        }
    }
    confirming?.let { selection ->
        AlertDialog(
            onDismissRequest = { confirming = null },
            title = { Text("清空${cacheTitle(selection.pool)}？") },
            text = { Text("将清空此连接保存在手机上的缩略图，媒体原件仍会保留。") },
            confirmButton = { TextButton(onClick = { confirming = null; onClear(selection) }) { Text("清空") } },
            dismissButton = { TextButton(onClick = { confirming = null }) { Text("取消") } },
        )
    }
}

private fun cacheTitle(pool: ThumbnailPool) = if (pool == ThumbnailPool.NAS) "服务器缓存" else "本地缓存"

@Composable
private fun CacheCard(
    pool: ThumbnailPool,
    usage: ThumbnailPoolUsage?,
    prefetch: ThumbnailPrefetchStatus?,
    coverage: ThumbnailCoverage?,
    clearing: ThumbnailPool?,
    onAskClear: (ThumbnailCacheSelection) -> Unit,
) {
    val title = cacheTitle(pool)
    QimengSettingsCard {
        Column(Modifier.fillMaxWidth().padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(title, style = MaterialTheme.typography.titleSmall)
            CacheInfoRow("文件数", usage?.usage?.let { "${it.files} 个" } ?: "—")
            CacheInfoRow("实际占用", usage?.usage?.let { formatBytes(it.bytes) } ?: "—")
            if (coverage != null && coverage.total > 0) {
                Text("生成进度", style = MaterialTheme.typography.titleSmall)
                val progress = coverage.covered.toDouble() / coverage.total
                val text = if (coverage.covered == coverage.total) "已全部生成 ${coverage.covered} / ${coverage.total}"
                else "已生成 ${coverage.covered} / ${coverage.total}（${(progress * 100).toInt()}%）"
                CacheStatus(text)
                LinearProgressIndicator(progress = { progress.toFloat() }, modifier = Modifier.fillMaxWidth())
            }
            if (prefetch != null) {
                Text("预取", style = MaterialTheme.typography.titleSmall)
                PrefetchProgress(prefetch)
            }
            TextButton(onClick = { usage?.selection?.let(onAskClear) }, enabled = clearing == null && usage?.selection != null) {
                Text(if (clearing == pool) "清空中…" else "清空$title")
            }
            Text(
                if (pool == ThumbnailPool.NAS) "连接服务器（NAS）时缓存的缩略图。文件数为缓存落盘数据文件数；与本地缓存分池存放。"
                else "连接本地端（手机内嵌服务端）时缓存的缩略图。与服务器缓存分池存放。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun CacheInfoRow(label: String, value: String) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(label, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
        CacheStatus(value)
    }
}

@Composable
private fun CacheStatus(text: String, failed: Boolean = false) {
    Text(text, style = MaterialTheme.typography.bodyMedium,
        color = if (failed) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant)
}

@Composable
private fun PrefetchProgress(prefetch: ThumbnailPrefetchStatus) {
    val text = when (prefetch) {
        ThumbnailPrefetchStatus.Idle -> "登录后自动预取全库缩略图，之后浏览直接读本地缓存"
        ThumbnailPrefetchStatus.WaitingForNetwork -> "等待非计费网络，连接后自动继续"
        is ThumbnailPrefetchStatus.Running -> {
            val progress = if (prefetch.total == 0L) 0f else (prefetch.cached.toDouble() / prefetch.total).toFloat()
            if (prefetch.total > 0) {
                CacheStatus("已缓存 ${prefetch.cached} / ${prefetch.total}（${(progress * 100).toInt()}%）")
                LinearProgressIndicator(progress = { progress }, modifier = Modifier.fillMaxWidth())
                return
            }
            "正在读取预取清单…"
        }
        is ThumbnailPrefetchStatus.Done -> when {
            prefetch.total == 0L -> "本轮完成，暂无可预取的缩略图"
            prefetch.atCapacity -> "已缓存 ${prefetch.cached} / ${prefetch.total}，缓存空间已用完"
            else -> "本轮完成，已缓存 ${prefetch.cached} / ${prefetch.total}"
        }
        ThumbnailPrefetchStatus.Skipped -> "库未变化，已跳过本轮预取（缓存已是最新）"
        is ThumbnailPrefetchStatus.Failed -> prefetch.message
    }
    CacheStatus(text, failed = prefetch is ThumbnailPrefetchStatus.Failed)
}
