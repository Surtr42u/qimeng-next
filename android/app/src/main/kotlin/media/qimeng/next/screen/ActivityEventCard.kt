package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.EventDeliveryKind
import media.qimeng.next.model.PendingEvent
import media.qimeng.next.model.sameTarget
import media.qimeng.next.ui.icon.HistoryRecentIcon
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

@Composable
internal fun ActivityEventCard(model: LibraryViewModel) {
    val queue by model.queue.collectAsStateWithLifecycle()
    val target by model.session.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val session = target
    val events = queue?.events.orEmpty()
    val owned = events.filter { sameTarget(it.target, session) }
    val review = owned.filter { it.issue != null || it.failure != null }
    val rejected = review.count { it.issue?.kind == EventDeliveryKind.REJECTED }
    val waiting = review.count { it.issue?.kind == EventDeliveryKind.WAITING && it.retryRequested != true }
    val unknown = review.count { it.retryRequested != true && (it.failure != null || it.issue?.kind == EventDeliveryKind.UNCONFIRMED) }
    val sendable = owned.size - rejected - waiting - unknown
    var details by remember(session) { mutableStateOf(false) }
    var confirm by remember(session) { mutableStateOf<PendingEvent?>(null) }
    MaintenanceCard(HistoryRecentIcon, "同步浏览数据") {
        Text(session?.label ?: "未连接服务器", style = MaterialTheme.typography.labelMedium)
        Text(if (queue == null) "正在读取本机记录…" else "待补传 $sendable 条", style = MaterialTheme.typography.bodySmall)
        if (waiting > 0) Text("等待恢复 $waiting 条", style = MaterialTheme.typography.bodySmall)
        if (rejected > 0 || unknown > 0) Text("已拒绝 $rejected 条 · 结果未确认 $unknown 条", style = MaterialTheme.typography.bodySmall)
        if (events.size > owned.size) Text("本机仍保留其他连接的 ${events.size - owned.size} 条记录", style = MaterialTheme.typography.bodySmall)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            TextButton(onClick = { session?.let { model.retryEvents(it) } }, enabled = session != null && (sendable > 0 || waiting > 0) && !busy) {
                Text("立即补传")
            }
            if (review.isNotEmpty()) TextButton(onClick = { details = true }) { Text("查看待处理记录") }
        }
    }
    if (details && session != null) AlertDialog(
        onDismissRequest = { details = false },
        title = { Text("浏览记录补传 · ${session.label}") },
        text = {
            LazyColumn(Modifier.heightIn(max = 400.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (review.isEmpty()) item { Text("没有待处理的记录") }
                items(review.asReversed(), key = PendingEvent::id) { event ->
                    Column(Modifier.testTag("activity-event:${event.id}"), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                        Text(event.assetName ?: event.assetId, style = MaterialTheme.typography.titleSmall)
                        Text(eventDescription(event), style = MaterialTheme.typography.bodySmall)
                        val reason = event.issue?.message ?: "旧版补传结果 · ${event.failure.orEmpty()}"
                        Text(reason, style = MaterialTheme.typography.bodySmall)
                        if (event.retryRequested == true) Text("等待重试", style = MaterialTheme.typography.bodySmall)
                        Row {
                            if (event.canRetry) TextButton(onClick = { model.retryEvents(session, event.id) }, enabled = !busy && event.retryRequested != true) {
                                Text("重试此条")
                            }
                            if (event.canDismiss) TextButton(onClick = { confirm = event }, enabled = !busy && event.retryRequested != true) {
                                Text("清理此条")
                            }
                        }
                        HorizontalDivider()
                    }
                }
            }
        },
        confirmButton = { TextButton(onClick = { details = false }) { Text("关闭") } },
    )
    confirm?.let { event ->
        if (session != null) AlertDialog(
            onDismissRequest = { confirm = null },
            title = { Text("清理这条补传记录？") },
            text = { Text("${event.assetName ?: event.assetId}\n${eventDescription(event)}\n确认后将从本机移除此条记录。") },
            confirmButton = { TextButton(onClick = { model.dismissEvent(session, event); confirm = null }, enabled = !busy) { Text("确认清理") } },
            dismissButton = { TextButton(onClick = { confirm = null }) { Text("取消") } },
        )
    }
}

private fun eventDescription(event: PendingEvent): String {
    val kind = when (event.kind) {
        "play" -> "播放"
        "progress" -> "续播进度"
        "dwell" -> "停留"
        else -> "浏览"
    }
    val time = runCatching { DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss").withZone(ZoneId.systemDefault()).format(Instant.parse(event.occurredAt)) }
        .getOrDefault(event.occurredAt)
    return "$kind · $time · 内容版本 ${event.contentVersion}"
}
