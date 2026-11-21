package media.qimeng.next.screen

import media.qimeng.next.model.Asset
import media.qimeng.next.model.Session
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 详情邻位原图预载准入与缓存键锁定（口径来源=旧版 DetailPreloadPolicy）：
 * 前 2 后 1、图片（含动图）且单张 ≤ 8 MiB、同实例、不含当前件；
 * 内存键必须与舞台请求逐字一致，否则预载解码结果不会命中舞台缓存槽。
 */
class DetailPreloadPolicyTest {

    @Test
    fun `窗口与前 2 后 1 对齐旧版`() {
        assertEquals(2, ORIGINAL_PRELOAD_FORWARD)
        assertEquals(1, ORIGINAL_PRELOAD_BACK)
        assertEquals(8L * 1024 * 1024, MAX_ORIGINAL_PRELOAD_BYTES)
    }

    @Test
    fun `内存键与舞台旧格式逐字一致`() {
        assertEquals("http://example.test:instance:a:7:original", originalImageMemoryKey(asset("a", version = 7)))
    }

    @Test
    fun `保留前 2 后 1 顺序并过滤不可预载项`() {
        val current = asset("current")
        val candidates = listOf(
            asset("next1"),
            asset("next2"),
            asset("prev1"),
            asset("video", mediaType = "video"),
            asset("huge", bytes = MAX_ORIGINAL_PRELOAD_BYTES + 1),
            asset("animated", mediaType = "animated_image"),
            asset("other-target", target = OTHER),
            asset("unknown-size", bytes = 0),
            asset("next1"),
        )
        assertEquals(
            listOf("next1", "next2", "prev1", "animated"),
            originalPreloadTargets(candidates, current).map { it.id },
        )
    }

    @Test
    fun `8MiB 边界包含_视频与未知大小排除`() {
        val current = asset("current")
        assertTrue(preloadableOriginal(asset("boundary", bytes = MAX_ORIGINAL_PRELOAD_BYTES), current))
        assertFalse(preloadableOriginal(asset("over", bytes = MAX_ORIGINAL_PRELOAD_BYTES + 1), current))
        assertFalse(preloadableOriginal(asset("video", mediaType = "video"), current))
        assertFalse(preloadableOriginal(asset("unknown", bytes = 0), current))
        assertFalse(preloadableOriginal(asset("current"), current))
        assertFalse(preloadableOriginal(asset("blank-url", url = ""), current))
        assertFalse(preloadableOriginal(asset("other-target", target = OTHER), current))
    }

    @Test
    fun `动图属于图片可预载`() {
        assertTrue(preloadableOriginal(asset("animated", mediaType = "animated_image"), asset("current")))
    }

    private fun asset(
        id: String,
        mediaType: String = "image",
        bytes: Long = 1024L,
        version: Long = 3,
        target: Session = SESSION,
        url: String = "http://example.test/$id",
    ) = Asset(
        id = id, libraryId = "lib", target = target, name = id, originalName = id,
        relativePath = "$id.jpg", mediaType = mediaType, version = version, revision = 1, tagsRevision = 1,
        bytes = bytes, width = 16, height = 9, durationMs = null, authorId = null, authorIds = emptyList(),
        authorsRevision = 1, authorName = null, tags = emptyList(), favorite = false, liked = false,
        likeCount = 0, positionMs = 0, positionVersion = 0, note = "", sourceIds = emptyList(),
        characterIds = emptyList(), acquisitionSources = emptyList(), collectionId = null, page = null,
        contentUrl = url, thumbnailUrl = "", addedAt = "2025-01-01T00:00:00Z",
    )

    private companion object {
        private val SESSION = Session(baseUrl = "http://example.test", instanceId = "instance", label = "test")
        private val OTHER = Session(baseUrl = "http://other.test", instanceId = "instance", label = "other")
    }
}
