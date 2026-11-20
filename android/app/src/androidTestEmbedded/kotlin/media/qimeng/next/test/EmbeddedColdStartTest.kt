package media.qimeng.next.test

import android.content.Intent
import android.database.sqlite.SQLiteDatabase
import android.graphics.Bitmap
import android.graphics.Color
import android.net.Uri
import android.os.Process
import androidx.lifecycle.ViewModelProvider
import androidx.test.platform.app.InstrumentationRegistry
import androidx.work.WorkManager
import java.io.File
import java.time.Instant
import java.util.UUID
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.MainActivity
import media.qimeng.next.QimengApplication
import media.qimeng.next.data.TransferWorker
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.model.PendingEvent
import media.qimeng.next.model.TransferState
import media.qimeng.next.model.UploadMode
import media.qimeng.next.runtime.RuntimePhase
import org.junit.Assert.*
import org.junit.Test

/** The hosted runner finishes seed instrumentation before stopping the target for recovery. */
class EmbeddedColdStartTest {
    private val instrumentation get() = InstrumentationRegistry.getInstrumentation()
    private val app get() = instrumentation.targetContext.applicationContext as QimengApplication
    private val marker get() = File(app.filesDir, "synthetic-cold-recovery.properties")

    private fun requireHostedFixture() {
        require(InstrumentationRegistry.getArguments().getString("qimengColdStart") == "true")
        require(app.runtime.available)
    }

    private fun launchActivity(): MainActivity = instrumentation.startActivitySync(
        Intent(instrumentation.targetContext, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    ) as MainActivity

    @Test fun seed() = runBlocking {
        requireHostedFixture()
        assertFalse("Hosted runner must start with clean synthetic data", marker.exists())
        val activity = launchActivity()
        try {
            app.localConnection.connect(forceLogin = true)
            val graph = app.graph
            val target = requireNotNull(graph.settings.session.value)
            val root = File(app.filesDir.canonicalFile, "embedded-v1/media/cold-recovery-fixture")
            assertTrue(root.mkdir() || root.isDirectory)
            val library = graph.media.registerLibrary(target, "Cold recovery fixture", root.absolutePath,
                false, "synthetic-cold-register-library")
            val uri = Uri.parse("content://media.qimeng.next.test.fixture/android-synthetic.png")
            val bitmap = Bitmap.createBitmap(40, 30, Bitmap.Config.ARGB_8888).apply { eraseColor(Color.BLUE) }
            try {
                requireNotNull(activity.contentResolver.openOutputStream(uri, "w")).use {
                    assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it))
                }
            } finally { bitmap.recycle() }
            val input = graph.uploads.receive(listOf(uri)).single()
            graph.uploads.enqueue(library.id, "", UploadMode.ORDINARY, inputIds = listOf(input))
            val transfer = withTimeout(80_000) {
                graph.queue.state.first { state ->
                    state?.transfers?.any { it.id == input && it.state == TransferState.SUCCEEDED } == true
                }!!.transfers.single { it.id == input }
            }
            val asset = graph.media.asset(requireNotNull(transfer.assetId))
            val event = PendingEvent(UUID.randomUUID().toString(), target, asset.id, "view", 0,
                asset.version, UUID.randomUUID().toString(), occurredAt = Instant.now().toString())
            // Do not let an existing upload-chain worker consume the seeded view.
            withTimeout(15_000) {
                while (WorkManager.getInstance(app).getWorkInfosByTag(TransferWorker::class.java.name)
                        .get(3, TimeUnit.SECONDS).any { !it.state.isFinished }) delay(100)
            }
            // Persist without invoking the scheduler. The new process must discover this exact intent.
            graph.queue.update { it.copy(events = it.events + event) }
            assertEquals(0, committedCount(event.id))
            marker.writeText(listOf(Process.myPid().toString(), target.instanceId,
                requireNotNull(graph.settings.certificateFor(target)), event.id, asset.id).joinToString("\n"))
        } finally {
            instrumentation.runOnMainSync { activity.finish() }
        }
    }

    @Test fun recover() = runBlocking {
        requireHostedFixture()
        val saved = marker.readLines()
        assertEquals(5, saved.size)
        assertNotEquals("Process must change; recreating Activity isn't sufficient", saved[0].toInt(), Process.myPid())
        val graph = app.graph
        val target = requireNotNull(graph.settings.session.value)
        assertEquals(saved[1], target.instanceId)
        assertEquals(saved[2], graph.settings.certificateFor(target))
        assertEquals(RuntimePhase.STOPPED, app.runtime.status.value.phase)
        assertFalse(app.transferConnectionReady(target))
        graph.awaitRecovery()
        assertTrue(requireNotNull(graph.queue.state.value).events.any { it.id == saved[3] })
        val problems = mutableListOf<String>()
        val watchQueue = launch(start = CoroutineStart.UNDISPATCHED) {
            graph.queue.error.collect { if (it != null) problems.add(it) }
        }
        var activity: MainActivity? = null
        var watchUi: Job? = null
        try {
            val screen = launchActivity().also { activity = it }
            lateinit var model: LibraryViewModel
            instrumentation.runOnMainSync { model = ViewModelProvider(screen)[LibraryViewModel::class.java] }
            val messages = mutableListOf<String>()
            watchUi = launch(start = CoroutineStart.UNDISPATCHED) {
                model.message.collect { if (it != null) messages.add(it) }
            }
            withTimeout(60_000) { app.localConnection.admitted.first { it?.instanceId == saved[1] } }
            assertTrue(app.transferConnectionReady(target))
            assertNotEquals("Cold native launch must renew the pinned certificate", saved[2], graph.settings.certificateFor(target))
            withTimeout(45_000) {
                graph.queue.state.first { it != null && it.events.none { event -> event.id == saved[3] } }
            }
            assertEquals("Exact event identity is committed once to its original catalog", 1, committedCount(saved[3]))
            assertTrue(graph.media.assets(AssetQuery(source = "history")).items.any { it.id == saved[4] && it.lastViewedAt != null })
            assertTrue("No transfer warning during first startup: $problems", problems.isEmpty())
            assertTrue("No premature connection failure: $messages",
                messages.none { it == "连接失败，请检查服务器后重试" })
            assertNull(model.message.value)
            assertNull(graph.queue.error.value)
        } finally {
            watchQueue.cancelAndJoin()
            watchUi?.cancelAndJoin()
            activity?.let { screen -> instrumentation.runOnMainSync { screen.finish() } }
        }
    }

    private fun committedCount(eventId: String): Int {
        val path = File(app.filesDir, "embedded-v1/control/qimeng.sqlite")
        return SQLiteDatabase.openDatabase(path.absolutePath, null, SQLiteDatabase.OPEN_READONLY).use { sql ->
            sql.rawQuery("SELECT COUNT(*) FROM events WHERE event_id = ?", arrayOf(eventId)).use { rows ->
                check(rows.moveToFirst())
                rows.getInt(0)
            }
        }
    }
}
