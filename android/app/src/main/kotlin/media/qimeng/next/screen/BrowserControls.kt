package media.qimeng.next.screen

import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable

/** Saved presentation choices; server requests remain in LibraryViewModel. */
@Stable
class BrowserControls(partition: String? = null, tag: String = "", columnCount: Int = 3) {
    val type = mutableStateOf<String?>(null)
    val partition = mutableStateOf(partition)
    val library = mutableStateOf<String?>(null)
    val sort = mutableStateOf("added_desc")
    val tag = mutableStateOf(tag)
    val columns = mutableIntStateOf(columnCount)
    val showFilters = mutableStateOf(false)
}

private val browserControlsSaver = listSaver<BrowserControls, Any>(
    save = { listOf(it.type.value.orEmpty(), it.partition.value.orEmpty(), it.library.value.orEmpty(), it.sort.value, it.tag.value, it.columns.intValue, it.showFilters.value) },
    restore = { values -> BrowserControls().apply {
        type.value = (values[0] as String).ifEmpty { null }; partition.value = (values[1] as String).ifEmpty { null }
        library.value = (values[2] as String).ifEmpty { null }; sort.value = values[3] as String; tag.value = values[4] as String
        columns.intValue = values[5] as Int; showFilters.value = values[6] as Boolean
    } })

@Composable
fun rememberBrowserControls(key: String, partition: String? = null, tag: String = "", columns: Int = 3): BrowserControls =
    rememberSaveable(key, saver = browserControlsSaver) { BrowserControls(partition, tag, columns) }
