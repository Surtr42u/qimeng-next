package media.qimeng.next.test

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.net.Uri
import android.os.ParcelFileDescriptor
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import media.qimeng.next.MainActivity
import media.qimeng.next.data.DataGraph
import media.qimeng.next.model.AssetQuery
import media.qimeng.next.model.TransferState
import media.qimeng.next.model.UploadMode
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import java.io.File

/** Run only against the same-SHA isolated Go process, using explicit cloud runner arguments. */
class AndroidRealServerTest {
    @get:Rule val compose = createAndroidComposeRule<MainActivity>()

    @Test(timeout = 300_000)
    fun loginUploadTagRecommendAndPlayThroughRealServer() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        val server = requireNotNull(args.getString("qimengServerUrl")) { "qimengServerUrl runner argument is required" }
        require(server == "http://127.0.0.1:18481/") { "Only the isolated cloud tunnel is allowed" }
        val token = requireNotNull(args.getString("qimengToken")) { "A synthetic server token is required" }
        val context = instrumentation.targetContext
        val graph = DataGraph.get(context)

        compose.onNodeWithText("HTTPS 服务器地址").performTextInput(server)
        compose.onNodeWithText("登录令牌").performTextInput(token)
        compose.onNodeWithText("连接影库").performClick()
        await { graph.settings.session.value != null && graph.queue.state.value != null }
        assertTrue("Cloud test requires an empty transfer journal", graph.queue.state.value!!.transfers.isEmpty())
        val library = runBlocking { graph.media.createLibrary("Android 云端合成影库", false) }

        val imageUri = Uri.parse("content://media.qimeng.next.test.fixture/android-synthetic.png")
        val videoUri = Uri.parse("content://media.qimeng.next.test.fixture/android-synthetic.mp4")
        val bitmap = Bitmap.createBitmap(160, 240, Bitmap.Config.ARGB_8888)
        Canvas(bitmap).apply {
            drawColor(Color.rgb(224, 180, 112))
            drawRect(0f, 0f, 80f, 240f, Paint().apply { color = Color.rgb(72, 112, 190) })
            drawCircle(80f, 120f, 46f, Paint().apply { color = Color.WHITE })
        }
        context.contentResolver.openOutputStream(imageUri, "w")!!.use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        bitmap.recycle()
        ParcelFileDescriptor.AutoCloseInputStream(instrumentation.uiAutomation.executeShellCommand("cat /data/local/tmp/qimeng-next-test.mp4")).use { input ->
            context.contentResolver.openOutputStream(videoUri, "w")!!.use { output ->
                val bytes = input.copyTo(output)
                assertTrue("Push a nonempty synthetic H.264 fixture before this test", bytes in 100L..8_000_000L)
            }
        }
        runBlocking {
            graph.uploads.receive(listOf(imageUri, videoUri))
            assertEquals(2, graph.queue.state.value!!.drafts.size)
            graph.uploads.enqueue(library.id, "android-cloud", UploadMode.ORDINARY)
        }
        await {
            val transfers = graph.queue.state.value!!.transfers
            assertTrue("A real upload failed: ${transfers.joinToString { it.message }}", transfers.none { it.state == TransferState.FAILED })
            transfers.size == 2 && transfers.all { it.state == TransferState.SUCCEEDED }
        }
        val assets = runBlocking { graph.media.assets(AssetQuery(libraryId = library.id)).items }
        assertEquals(2, assets.size)
        val picture = assets.single { it.mediaType == "image" }
        val video = assets.single { it.mediaType == "video" }
        assertTrue(picture.thumbnailUrl.isNotEmpty())
        assertTrue((video.durationMs ?: 0) > 0)

        compose.onNodeWithText("首页").performClick()
        compose.onNodeWithContentDescription("刷新").performClick()
        compose.waitUntil(30_000) { compose.onAllNodesWithContentDescription(picture.name).fetchSemanticsNodes().isNotEmpty() }
        screenshot("real-server-home.png")
        compose.onNodeWithContentDescription(picture.name).performClick()
        compose.waitUntil(30_000) { compose.onAllNodesWithText("管理标签").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("管理标签").performScrollTo().performClick()
        compose.onNodeWithText("新建标签").performTextInput("安卓云端标签")
        compose.onNodeWithText("添加").performClick()
        compose.waitUntil(20_000) { compose.onAllNodesWithText("安卓云端标签").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("保存").performClick()
        await { graph.media.asset(picture.id).tags == listOf("安卓云端标签") }
        compose.onNodeWithText("收藏").performScrollTo().performClick()
        await { graph.media.asset(picture.id).favorite }
        screenshot("real-server-detail.png")

        compose.onNodeWithContentDescription("返回").performClick()
        compose.onNodeWithContentDescription(video.name).performClick()
        compose.waitUntil(30_000) { compose.onAllNodesWithContentDescription("进入全屏").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithContentDescription("视频播放区域").performTouchInput { doubleClick(center) }
        await { graph.media.history().items.any { it.assetId == video.id && it.kind == "play" } }
        compose.onNodeWithContentDescription("进入全屏").performClick()
        compose.onNodeWithContentDescription("退出全屏").assertIsDisplayed()
        screenshot("real-server-video-fullscreen.png")
        compose.onNodeWithContentDescription("退出全屏").performClick()
        val recommendation = runBlocking { graph.media.assets(AssetQuery(source = "recommendations", seed = 42)) }
        assertTrue(recommendation.items.any { it.id == picture.id })
    }

    private fun await(condition: suspend () -> Boolean) = runBlocking {
        withTimeout(120_000) { while (!condition()) delay(400) }
    }
    private fun screenshot(name: String) {
        val directory = InstrumentationRegistry.getInstrumentation().targetContext.getExternalFilesDir("screenshots")!!
        directory.mkdirs()
        compose.waitForIdle()
        val screenshot = requireNotNull(InstrumentationRegistry.getInstrumentation().uiAutomation.takeScreenshot())
        File(directory, name).outputStream().use { screenshot.compress(Bitmap.CompressFormat.PNG, 100, it) }
        screenshot.recycle()
    }
}
