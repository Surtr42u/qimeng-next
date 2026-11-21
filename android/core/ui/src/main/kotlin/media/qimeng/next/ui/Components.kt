package media.qimeng.next.ui

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

@Composable
fun ContentCard(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    Surface(modifier, shape = RoundedCornerShape(22.dp), color = MaterialTheme.colorScheme.surface, shadowElevation = 1.dp) {
        Column(Modifier.padding(18.dp), verticalArrangement = Arrangement.spacedBy(12.dp), content = content)
    }
}

/**
 * [ErrorNotice] 的可选第二动作（标签 + 回调），例如连接失败时的「连接本机影库」。
 * 与重试按钮并排显示，调用方只在确实有替代出口时传入。
 */
data class ErrorNoticeAction(val label: String, val onClick: () -> Unit)

/** 原有的双参数入口保持不变：最后一个参数是函数类型，所有 `ErrorNotice(message) { … }` 写法继续有效。 */
@Composable
fun ErrorNotice(message: String, onRetry: (() -> Unit)? = null) {
    ErrorNoticeWithActions(message, onRetry, "重试", null)
}

/**
 * 带自定义重试文案与第二动作的通知。四个参数都必须显式传入，避免与 [ErrorNotice] 的重载
 * 产生歧义；[onRetry] 可以为 null（只显示第二动作）。
 */
@Composable
fun ErrorNoticeWithActions(
    message: String,
    onRetry: (() -> Unit)?,
    retryLabel: String,
    secondary: ErrorNoticeAction?,
) {
    Surface(color = MaterialTheme.colorScheme.errorContainer, shape = RoundedCornerShape(16.dp)) {
        Column(Modifier.fillMaxWidth().padding(14.dp)) {
            Text(message, color = MaterialTheme.colorScheme.onErrorContainer)
            if (onRetry != null || secondary != null) Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (onRetry != null) TextButton(onClick = onRetry) { Text(retryLabel) }
                secondary?.let { TextButton(onClick = it.onClick) { Text(it.label) } }
            }
        }
    }
}
