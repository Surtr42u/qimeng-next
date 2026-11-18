package media.qimeng.next.ui.component

import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import media.qimeng.next.ui.icon.BackIcon

/** Original pure TopAppBar presentation; the caller owns navigation and data. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun QimengTopBar(title: String, modifier: Modifier = Modifier, onBack: (() -> Unit)? = null,
                 actions: @Composable () -> Unit = {}) {
    TopAppBar(title = { Text(title, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.SemiBold) },
        navigationIcon = { if (onBack != null) IconButton(onClick = onBack) { Icon(BackIcon, "返回") } },
        actions = { actions() }, colors = TopAppBarDefaults.topAppBarColors(containerColor = MaterialTheme.colorScheme.background),
        modifier = modifier)
}
