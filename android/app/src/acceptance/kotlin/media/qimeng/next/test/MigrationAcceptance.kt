package media.qimeng.next.test

import android.app.Activity
import android.app.Application
import android.app.Instrumentation
import android.content.Context
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.os.Bundle
import android.os.Environment
import android.os.Process
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import media.qimeng.next.QimengApplication
import media.qimeng.next.data.MediaRepository
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget
import media.qimeng.next.runtime.RuntimePhase
import okhttp3.Request
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.nio.file.Files
import java.nio.file.LinkOption
import java.security.MessageDigest

/** Owner-signed, API 35 instrumentation attached to the running Next process with --no-restart. */
class MigrationAcceptance : Instrumentation() {
    private val result = Bundle()
    private var command: String? = null

    override fun onCreate(arguments: Bundle?) {
        super.onCreate(arguments)
        command = arguments?.getString("command")
        start()
    }

    override fun onStart() {
        val ok = try {
            runBlocking { withTimeout(240_000) { execute() } }
            true
        } catch (_: Throwable) {
            // A transport exception, JSON error or framework message could contain private data.
            false
        }
        result.putBoolean("ok", ok)
        finish(if (ok) Activity.RESULT_OK else Activity.RESULT_CANCELED, result)
    }

    private suspend fun execute() {
        verify("validCommand", command == "quiesce" || command == "accept")
        val app = targetContext.applicationContext as QimengApplication
        val target = "media.qimeng.next"
        verify("targetProcess", Application.getProcessName() == target && app.packageName == target &&
            app.applicationInfo.uid == Process.myUid() && app.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0)
        verify("sameSignature", app.packageManager.checkSignatures(target, "$target.test") == PackageManager.SIGNATURE_MATCH)

        val runtime = app.runtime
        val settings = app.graph.settings
        val selected = settings.session.value
        verify("selectedLocal", runtime.available && selected != null && selected.baseUrl == runtime.baseUrl &&
            selected.baseUrl in runtime.ownedEndpoints(selected.instanceId))
        requireNotNull(selected)
        val mode = settings.defaultEndpoint.value
        val state = app.getSharedPreferences("qimeng-migration-acceptance", Context.MODE_PRIVATE)
        val wasEnabled = state.getBoolean("restoreEnabled", runtime.shouldRestore)
        result.putBoolean("previouslyEnabled", wasEnabled)

        if (command == "quiesce") {
            // The first stop owns the original preference even if the caller retries after a disconnect.
            if (!state.contains("restoreEnabled")) check(state.edit().putBoolean("restoreEnabled", wasEnabled).commit())
            runtime.stop()
            verify("stopped", runtime.status.value.phase == RuntimePhase.STOPPED && !runtime.shouldRestore)
        } else {
            val expected = readSelection(app)
            verify("expectedInstance", expected.getString("instanceId") == selected.instanceId)
            val samples = expected.getJSONArray("originals")
            verify("selectedSamples", samples.length() in 1..3)
            val db = File(app.filesDir, "embedded-v1/control/qimeng.sqlite")
            verify("existingDatabase", Files.isRegularFile(db.toPath(), LinkOption.NOFOLLOW_LINKS) && db.length() > 0)
            runtime.files.refreshAccess()
            verify("sharedStorageGranted", runtime.files.access.value.granted)
            try {
                result.putBoolean("runtimeReady", false)
                val handoff = runtime.start()
                verify("runtimeReady", runtime.status.value.phase == RuntimePhase.READY &&
                    handoff.instanceId == selected.instanceId && handoff.baseUrl == selected.baseUrl)
                // The product coordinator refreshes an expired leaf pin through its normal session exchange.
                app.localConnection.connect(forceLogin = false)
                verify("trustedLocalSession", sameTarget(selected, settings.session.value) &&
                    settings.certificateFor(selected) == handoff.certificateDerBase64)
                val media = app.graph.media
                media.capabilities()
                val before = media.statistics()
                val counts = mapOf("assets" to before.assets, "authors" to before.authors, "events" to before.events,
                    "images" to before.images, "videos" to before.videos, "favorites" to before.favorites,
                    "bytes" to before.bytes, "trash" to before.trash)
                counts.forEach { (name, value) -> result.putLong(name, value) }
                val expectedCounts = if (expected.has("counts")) expected.getJSONObject("counts") else JSONObject()
                verify("countsMatch", expectedCounts.keys().asSequence().all { counts[it] == expectedCounts.getLong(it) })
                for (index in 0 until samples.length()) {
                    checkOriginal(media, selected, samples.getJSONObject(index))
                    result.putInt("originalsVerified", index + 1)
                }
                verify("eventsUnchanged", media.statistics().events == before.events)
            } finally {
                if (!wasEnabled) runtime.stop()
                verify("lifecycleRestored", runtime.shouldRestore == wasEnabled &&
                    runtime.status.value.phase == if (wasEnabled) RuntimePhase.READY else RuntimePhase.STOPPED)
            }
        }
        verify("selectionPreserved", sameTarget(selected, settings.session.value) && settings.defaultEndpoint.value == mode)
        if (command == "accept" && state.contains("restoreEnabled")) check(state.edit().remove("restoreEnabled").commit())
    }

    private fun readSelection(app: Context): JSONObject {
        val file = File(app.cacheDir, "qimeng-migration-check.json")
        verify("manifestPresent", Files.isRegularFile(file.toPath(), LinkOption.NOFOLLOW_LINKS) && file.length() in 1..32768)
        return JSONObject(file.readText(Charsets.UTF_8))
    }

    @Suppress("DEPRECATION")
    private suspend fun checkOriginal(media: MediaRepository, session: Session, expected: JSONObject) {
        val id = expected.getString("id")
        val size = expected.getLong("bytes")
        val hash = expected.getString("sha256")
        verify("validSample", id.isNotBlank() && size in 1..(64L * 1024 * 1024) && hash.matches(Regex("[0-9a-f]{64}")))
        val asset = media.asset(id, session)
        verify("assetMatches", asset.id == id && asset.version > 0 && asset.bytes == size)
        if (expected.has("authorIds")) {
            val authors = expected.getJSONArray("authorIds").strings()
            verify("authorRelationsMatch", authors == asset.authorIds.toSet() && authors.size <= 64)
            verify("authorsReadable", authors.all { media.author(it, session).id == it })
        }
        if (expected.has("tags")) verify("tagsMatch", expected.getJSONArray("tags").strings() == asset.tags.toSet())

        val primary = Environment.getExternalStorageDirectory().canonicalFile.toPath()
        val library = File(media.libraryRoot(session, asset.libraryId)).canonicalFile
        verify("sharedLibrary", library.toPath().startsWith(primary) && library.isDirectory)
        val request = Request.Builder().url(asset.contentUrl).header("Accept-Encoding", "identity").get().build()
        media.mediaClient(session).newCall(request).execute().use { response ->
            verify("versionedOriginal", response.code == 200 && response.header("ETag") == "\"$hash\"")
            val body = requireNotNull(response.body)
            verify("sizeMatches", body.contentLength() == size)
            val digest = MessageDigest.getInstance("SHA-256")
            val buffer = ByteArray(64 * 1024)
            var bytes = 0L
            body.byteStream().use { input ->
                while (true) {
                    val n = input.read(buffer)
                    if (n < 0) break
                    bytes += n
                    check(bytes <= size)
                    digest.update(buffer, 0, n)
                }
            }
            val expectedDigest = hash.chunked(2).map { it.toInt(16).toByte() }.toByteArray()
            verify("digestMatches", bytes == size && MessageDigest.isEqual(digest.digest(), expectedDigest))
        }
    }

    private fun JSONArray.strings(): Set<String> = (0 until length()).map { getString(it) }.toSet()

    private fun verify(label: String, condition: Boolean) {
        result.putBoolean(label, condition)
        check(condition)
    }
}
