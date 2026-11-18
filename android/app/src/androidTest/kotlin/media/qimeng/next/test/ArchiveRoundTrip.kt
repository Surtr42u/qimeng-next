package media.qimeng.next.test

import android.content.Intent
import android.net.Uri
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.delay
import kotlinx.coroutines.withTimeout
import media.qimeng.next.data.DataGraph
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.model.TransferState
import org.junit.Assert.*

/** Real provider grants, directory resolver, SQLite intake journal, Android workers, Go storage and protection receipt. */
internal suspend fun archiveRoundTrip(graph: DataGraph) {
    val instrumentation = InstrumentationRegistry.getInstrumentation()
    val context = instrumentation.targetContext
    val automation = instrumentation.uiAutomation
    val authority = Uri.parse("content://media.qimeng.next.test.archive")
    automation.adoptShellPermissionIdentity("android.permission.MANAGE_DOCUMENTS")
    val tree = try {
        val result = requireNotNull(context.contentResolver.call(authority, "seedTestArchive", null, null))
        Uri.parse(requireNotNull(result.getString("tree")))
    } finally { automation.dropShellPermissionIdentity() }
    val target = requireNotNull(graph.settings.session.value)
    val library = graph.media.createLibrary("云端归档目标", false)
    graph.archives.select(tree, Intent.FLAG_GRANT_READ_URI_PERMISSION)
    val preview = graph.archives.scan(target, local = false)
    assertEquals(1L, preview.files)
    assertEquals(1L, preview.excludedFolders)
    assertEquals(listOf("未登记合成目录"), preview.examples)
    graph.archives.confirm(preview)
    val transfer = withTimeout(150_000) {
        while (true) {
            val item = graph.queue.state.value?.transfers?.find { it.libraryId == library.id }
            check(item?.state != TransferState.FAILED && item?.archiveSourceState != "blocked") { "Archive stage failed: ${item?.message}" }
            if (item?.archiveSourceState == "done") break
            delay(300)
        }
        graph.queue.state.value!!.transfers.single { it.libraryId == library.id }
    }
    assertEquals(TransferState.SUCCEEDED, transfer.state)
    assertTrue("Read-only provider must have a confirmed non-recycle outcome", transfer.message.contains("未执行回收"))
    assertEquals("合成子目录/归档合成.png", graph.media.assets(AssetQuery(libraryId = library.id)).items.single().relativePath)
    val original = android.provider.DocumentsContract.buildDocumentUriUsingTree(tree, "root/library/album/png")
    context.contentResolver.openInputStream(original)!!.use { assertTrue("The source remains accessible", it.read() >= 0) }
    assertEquals("queued", graph.archives.latest(target)?.state)
    assertEquals("An unchanged archive must not enqueue another upload", preview.id, graph.archives.scan(target, local = false).id)
}
