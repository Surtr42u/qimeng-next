package media.qimeng.next.cloudfixture

import android.app.Activity
import android.app.Instrumentation
import android.graphics.Bitmap
import android.os.Build
import android.os.Bundle
import android.os.Environment
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import media.qimeng.next.QimengApplication
import media.qimeng.next.model.AssetQuery
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.security.MessageDigest

/** Separate cloud-only APK. It seeds a brand-new emulator using the installed app's real repositories. */
class CloudFixture : Instrumentation() {
    private var requested = false
    override fun onCreate(arguments: Bundle?) {
        super.onCreate(arguments)
        requested = arguments?.getString("synthetic") == "true"
        start()
    }

    @Suppress("DEPRECATION")
    override fun onStart() {
        val result = Bundle()
        val success = try {
            runBlocking {
                withTimeout(180_000) {
                    check(requested && Build.HARDWARE in setOf("ranchu", "goldfish") && Build.VERSION.SDK_INT == 35)
                    val app = targetContext.applicationContext as QimengApplication
                    val media = app.graph.media
                    check(app.packageName == "media.qimeng.next" && media.session.value == null)
                    check(!File(app.filesDir, "embedded-v1").exists())
                    app.localConnection.connect(forceLogin = true)
                    result.putBoolean("connected", true)
                    val session = requireNotNull(media.session.value)
                    val folder = File(Environment.getExternalStorageDirectory(), "QimengMigrationSynthetic")
                    check(folder.mkdir())
                    val file = File(folder, "cloud.png")
                    val image = Bitmap.createBitmap(16, 16, Bitmap.Config.ARGB_8888)
                    try {
                        image.eraseColor(0xff3874ab.toInt())
                        file.outputStream().use { check(image.compress(Bitmap.CompressFormat.PNG, 100, it)) }
                    } finally {
                        image.recycle()
                    }
                    val library = media.registerLibrary(session, "Cloud fixture", folder.canonicalPath, false, "cloud-migration-library")
                    app.graph.scans.start(session, library, "cloud-migration-scan")
                    val query = AssetQuery(libraryId = library.id)
                    while (media.assets(query).items.isEmpty()) delay(250)
                    val asset = media.assets(query).items.single()
                    val author = media.createAuthor("Cloud fixture author")
                    val attributed = media.updateAuthors(asset, listOf(author.id), emptyList(), asset.revision, asset.authorsRevision)
                    val tagged = media.tags(attributed, listOf("cloud-tag"))
                    check(tagged.authorIds == listOf(author.id) && tagged.tags == listOf("cloud-tag"))
                    val stats = media.statistics()
                    check(stats.assets == 1L)
                    val hash = MessageDigest.getInstance("SHA-256").digest(file.readBytes()).joinToString("") { "%02x".format(it) }
                    val sample = JSONObject().put("id", tagged.id).put("bytes", file.length()).put("sha256", hash)
                        .put("authorIds", JSONArray(tagged.authorIds)).put("tags", JSONArray(tagged.tags))
                    val expected = JSONObject().put("instanceId", session.instanceId).put("originals", JSONArray().put(sample))
                        .put("counts", JSONObject().put("assets", stats.assets).put("authors", stats.authors).put("events", stats.events))
                    File(app.cacheDir, "cloud-check-good.json").writeText(expected.toString())
                    sample.put("sha256", "0".repeat(64))
                    File(app.cacheDir, "cloud-check-bad.json").writeText(expected.toString())
                }
            }
            true
        } catch (failure: Exception) {
            // Only the exception type is appropriate even in the synthetic setup log.
            result.putString("fixtureFailure", failure.javaClass.simpleName)
            false
        }
        result.putBoolean("seeded", success)
        finish(if (success) Activity.RESULT_OK else Activity.RESULT_CANCELED, result)
    }
}
