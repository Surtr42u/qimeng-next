package media.qimeng.next.ui

import androidx.compose.runtime.staticCompositionLocalOf

/** User motion preference is supplied by the app; reusable presentation has no settings dependency. */
val LocalReducedMotion = staticCompositionLocalOf { false }
