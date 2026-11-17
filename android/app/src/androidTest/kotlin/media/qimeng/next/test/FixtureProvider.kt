package media.qimeng.next.test

import android.content.ContentProvider
import android.content.ContentValues
import android.database.Cursor
import android.database.MatrixCursor
import android.net.Uri
import android.os.ParcelFileDescriptor
import android.provider.OpenableColumns
import java.io.File

/** Cloud-only test APK. It exposes exactly two synthetic fixtures, never user directories. */
class FixtureProvider : ContentProvider() {
    override fun onCreate() = true
    private fun file(uri: Uri): File {
        val name = uri.lastPathSegment
        require(name in setOf("android-synthetic.png", "android-synthetic.mp4"))
        return File(requireNotNull(context).filesDir, "fixture-$name")
    }
    // Legal file providers may omit MIME; the real server must classify the synthetic PNG.
    override fun getType(uri: Uri): String? = if (uri.lastPathSegment?.endsWith(".mp4") == true) "video/mp4" else null
    override fun openFile(uri: Uri, mode: String): ParcelFileDescriptor {
        val flags = if (mode == "w" || mode == "wt") ParcelFileDescriptor.MODE_CREATE or ParcelFileDescriptor.MODE_WRITE_ONLY or ParcelFileDescriptor.MODE_TRUNCATE
        else ParcelFileDescriptor.MODE_READ_ONLY
        return ParcelFileDescriptor.open(file(uri), flags)
    }
    override fun query(uri: Uri, projection: Array<out String>?, selection: String?, selectionArgs: Array<out String>?, sortOrder: String?): Cursor {
        val columns = projection ?: arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE)
        val f = file(uri)
        return MatrixCursor(columns).apply { addRow(columns.map { when (it) { OpenableColumns.DISPLAY_NAME -> uri.lastPathSegment; OpenableColumns.SIZE -> f.length(); else -> null } }) }
    }
    override fun insert(uri: Uri, values: ContentValues?): Uri? = throw UnsupportedOperationException()
    override fun delete(uri: Uri, selection: String?, selectionArgs: Array<out String>?): Int = throw UnsupportedOperationException()
    override fun update(uri: Uri, values: ContentValues?, selection: String?, selectionArgs: Array<out String>?): Int = throw UnsupportedOperationException()
}
