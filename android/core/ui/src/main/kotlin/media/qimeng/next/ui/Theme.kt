package media.qimeng.next.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.Composable

@Composable
fun QimengTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    media.qimeng.next.ui.theme.QimengTheme(darkTheme = dark, content = content)
}
