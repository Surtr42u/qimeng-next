package media.qimeng.next.test

import android.app.Activity
import android.graphics.Bitmap
import android.graphics.Color
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.PixelCopy
import android.view.SurfaceView
import android.view.View
import androidx.media3.ui.PlayerView
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeoutOrNull
import media.qimeng.next.R
import media.qimeng.next.testsupport.saveCloudBitmap
import kotlin.coroutines.resume

internal fun saturatedSamples(image: Bitmap): Int {
    var colored = 0
    for (y in image.height / 8 until image.height * 7 / 8 step 8) for (x in 8 until image.width - 8 step 8) {
        val pixel = image.getPixel(x, y)
        val r = Color.red(pixel); val g = Color.green(pixel); val b = Color.blue(pixel)
        if (maxOf(r, g, b) - minOf(r, g, b) > 75) colored++
    }
    return colored
}

private data class SurfaceFrame(val description: String, val image: Bitmap? = null)

/** Sample the real decoder layer separately so a black composited window can be diagnosed. */
internal fun captureDecoderSurface(activity: Activity, suffix: String) {
    var description = "no surface"
    var best: Bitmap? = null
    var samples = -1
    try {
        runBlocking {
            withTimeoutOrNull(3_000) {
                while (samples < 30) {
                    val frame = copyDecoderSurface(activity)
                    description = frame.description
                    frame.image?.let { image ->
                        val count = saturatedSamples(image)
                        if (count > samples) { best?.recycle(); best = image; samples = count } else image.recycle()
                    }
                    if (samples < 30) delay(150)
                }
            }
        }
        best?.let { saveCloudBitmap("diagnostic-video-decoder-surface$suffix.png", it) }
        Log.i("QimengFrameEvidence", "decoderSurface$suffix $description saturatedSamples=$samples")
    } finally { best?.recycle() }
}

@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
private suspend fun copyDecoderSurface(activity: Activity): SurfaceFrame = suspendCancellableCoroutine { pending ->
    InstrumentationRegistry.getInstrumentation().runOnMainSync {
        val player = activity.findViewById<PlayerView>(R.id.video_playback_window)
        val view = player?.videoSurfaceView as? SurfaceView
        val rect = view?.holder?.surfaceFrame
        if (view == null || !view.holder.surface.isValid || rect == null || rect.isEmpty) {
            pending.resume(SurfaceFrame("not attached"))
            return@runOnMainSync
        }
        val shutter = player?.findViewById<View>(androidx.media3.ui.R.id.exo_shutter)
        val description = "${rect.width()}x${rect.height()} view=${view.width}x${view.height} shutterVisible=${shutter?.isShown}"
        val frame = Bitmap.createBitmap(rect.width(), rect.height(), Bitmap.Config.ARGB_8888)
        try {
            PixelCopy.request(view, frame, { result ->
                if (result == PixelCopy.SUCCESS && pending.isActive) pending.resume(SurfaceFrame("$description pixelCopy=$result", frame))
                else {
                    frame.recycle()
                    if (pending.isActive) pending.resume(SurfaceFrame("$description pixelCopy=$result"))
                }
            }, Handler(Looper.getMainLooper()))
        } catch (_: IllegalArgumentException) {
            frame.recycle()
            if (pending.isActive) pending.resume(SurfaceFrame("$description surface changed"))
        }
    }
}
