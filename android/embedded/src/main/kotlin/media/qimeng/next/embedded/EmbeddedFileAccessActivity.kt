package media.qimeng.next.embedded

import android.Manifest
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.Settings

/** Permission handoff belongs only to the optional host, and is launched by a user action. */
class EmbeddedFileAccessActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (FileAccessState.granted(this)) { complete(); return }
        if (savedInstanceState != null) return
        if (Build.VERSION.SDK_INT >= 30) {
            try {
                @Suppress("DEPRECATION")
                startActivityForResult(Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION, Uri.parse("package:$packageName")), REQUEST)
            } catch (_: ActivityNotFoundException) {
                try {
                    @Suppress("DEPRECATION")
                    startActivityForResult(Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION), REQUEST)
                } catch (_: ActivityNotFoundException) { FileAccessState.unavailable(); finish() }
            }
        } else requestPermissions(arrayOf(Manifest.permission.READ_EXTERNAL_STORAGE, Manifest.permission.WRITE_EXTERNAL_STORAGE), REQUEST)
    }

    @Deprecated("Framework result handoff is confined to this optional permission activity")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == REQUEST) complete()
    }
    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == REQUEST) complete()
    }
    private fun complete() { FileAccessState.refresh(this); finish() }
    private companion object { const val REQUEST = 1 }
}
