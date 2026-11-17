package media.qimeng.next.ui

import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/** Original dock geometry, growing only when the user's larger type needs the extra height. */
object NavigationDimensions {
    val horizontalMargin = 16.dp
    val bottomMargin = 10.dp
    val slotPadding = 18.dp
    @Composable fun height(): Dp = with(LocalDensity.current) {
        maxOf(62.dp, 24.dp + 2.dp + MaterialTheme.typography.labelMedium.lineHeight.toDp() + 8.dp)
    }
    @Composable fun bottomClearance(): Dp = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding() + maxOf(80.dp, height() + bottomMargin)
}
