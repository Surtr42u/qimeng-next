package media.qimeng.next.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

private val LightColors = lightColorScheme(
    primary = Color(0xFF6750A4), onPrimary = Color.White,
    primaryContainer = Color(0xFFEADDFF), onPrimaryContainer = Color(0xFF28124F),
    background = Color(0xFFF1F2F4), onBackground = Color(0xFF202124),
    surface = Color.White, onSurface = Color(0xFF202124),
    surfaceVariant = Color(0xFFE9E9ED), onSurfaceVariant = Color(0xFF55545B),
)
private val DarkColors = darkColorScheme(
    primary = Color(0xFFD0BCFF), onPrimary = Color(0xFF381E72),
    primaryContainer = Color(0xFF4F378B), onPrimaryContainer = Color(0xFFEADDFF),
    background = Color(0xFF141414), onBackground = Color(0xFFF3F1F5),
    surface = Color(0xFF242424), onSurface = Color(0xFFF3F1F5),
    surfaceVariant = Color(0xFF333236), onSurfaceVariant = Color(0xFFCAC5CF),
)

@Composable
fun QimengTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = if (dark) DarkColors else LightColors, content = content)
}
