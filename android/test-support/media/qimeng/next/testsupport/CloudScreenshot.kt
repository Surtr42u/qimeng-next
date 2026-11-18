package media.qimeng.next.testsupport

import android.content.ContentValues
import android.graphics.Bitmap
import android.os.Build
import android.provider.MediaStore
import androidx.test.platform.app.InstrumentationRegistry

/** The cloud runner pulls this synthetic album after Gradle uninstalls the test packages. */
fun captureCloudScreenshot(name: String, accept: (Bitmap) -> Boolean = { true }): Boolean {
    require(Build.VERSION.SDK_INT >= 29)
    val instrumentation = InstrumentationRegistry.getInstrumentation()
    val bitmap = requireNotNull(instrumentation.uiAutomation.takeScreenshot())
    val resolver = instrumentation.targetContext.contentResolver
    val values = ContentValues().apply {
        put(MediaStore.MediaColumns.DISPLAY_NAME, name)
        put(MediaStore.MediaColumns.MIME_TYPE, "image/png")
        put(MediaStore.MediaColumns.RELATIVE_PATH, "Pictures/QimengNextCloudTests")
        put(MediaStore.MediaColumns.IS_PENDING, 1)
    }
    try {
        if (!accept(bitmap)) return false
        val uri = requireNotNull(resolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values))
        try {
            requireNotNull(resolver.openOutputStream(uri)).use { check(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
            values.clear()
            values.put(MediaStore.MediaColumns.IS_PENDING, 0)
            check(resolver.update(uri, values, null, null) == 1)
            return true
        } catch (failure: Exception) {
            resolver.delete(uri, null, null)
            throw failure
        }
    } finally { bitmap.recycle() }
}
