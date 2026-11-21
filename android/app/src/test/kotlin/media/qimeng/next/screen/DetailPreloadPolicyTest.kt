package media.qimeng.next.screen

import media.qimeng.next.adjacentAssetIndex
import media.qimeng.next.model.Asset
import media.qimeng.next.model.Session
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 详情邻位预载、lg 预览与邻位切换边界锁定（口径来源=旧版 DetailPreloadPolicy /
 * ZoomImageView 相邻件切换）：前 2 后 1 窗口内按 +1、-1、+2 近者优先；原图准入为
 * 图片（含动图）且单张 ≤ 8 MiB、8 MP、同实例、不含当前件；lg 预览只看同实例/非视频，
 * 不受原件大小限制。内存键必须与舞台请求逐字一致，否则预载解码结果不会命中舞台缓存槽。
 * 另锁定详情首帧 lg 预览：地址固定 size=lg、旧 size 参数被替换、空地址无预览，
 * 内存键与网格 md 槽/原图槽分开。以及当前件不在已加载清单（index=-1）时邻位切换不动。
 */
class DetailPreloadPolicyTest {

    @Test
    fun `窗口与前 2 后 1 对齐旧版`() {
        assertEquals(2, ORIGINAL_PRELOAD_FORWARD)
        assertEquals(1, ORIGINAL_PRELOAD_BACK)
        assertEquals(8L * 1024 * 1024, MAX_ORIGINAL_PRELOAD_BYTES)
        assertEquals(2, PREVIEW_PRELOAD_CONCURRENCY)
    }

    @Test
    fun `内存键与舞台旧格式逐字一致`() {
        assertEquals("http://example.test:instance:a:7:original", originalImageMemoryKey(asset("a", version = 7)))
    }

    @Test
    fun `邻位预载顺序为 +1 -1 +2 近者优先`() {
        val ahead = listOf(asset("next1"), asset("next2"))
        val behind = listOf(asset("prev1"), asset("prev2"))
        // 旧版 DetailPreloadPolicy.computePreload 的 orderedDeltas = listOf(1, -1, 2)。
        assertEquals(
            listOf("next1", "prev1", "next2", "prev2"),
            neighbourPreloadOrder(ahead, behind).map { it.id },
        )
        // 详情窗口前 2 后 1 时正是旧版的 [1, -1, 2]。
        assertEquals(
            listOf("next1", "prev1", "next2"),
            neighbourPreloadOrder(ahead, behind.take(1)).map { it.id },
        )
        // 清单首尾自然变短、不环绕；重复 id 只保留先出现的一次。
        assertEquals(listOf("next1"), neighbourPreloadOrder(listOf(asset("next1")), emptyList()).map { it.id })
        assertEquals(listOf("prev1"), neighbourPreloadOrder(emptyList(), listOf(asset("prev1"))).map { it.id })
        assertEquals(
            listOf("next1", "prev1"),
            neighbourPreloadOrder(listOf(asset("next1")), listOf(asset("next1"), asset("prev1"))).map { it.id },
        )
    }

    @Test
    fun `邻位原图清单保持输入顺序并过滤不可预载项`() {
        val current = asset("current")
        // 调用方已按 neighbourPreloadOrder 排成 +1、-1、+2；这里锁定过滤不重排。
        val candidates = listOf(
            asset("next1"),
            asset("prev1"),
            asset("next2"),
            asset("video", mediaType = "video"),
            asset("huge", bytes = MAX_ORIGINAL_PRELOAD_BYTES + 1),
            asset("animated", mediaType = "animated_image"),
            asset("other-target", target = OTHER),
            asset("unknown-size", bytes = 0),
            asset("next1"),
        )
        assertEquals(
            listOf("next1", "prev1", "next2", "animated"),
            originalPreloadTargets(candidates, current).map { it.id },
        )
    }

    @Test
    fun `lg 预览预载同实例非视频且不按原件大小限制`() {
        val current = asset("current")
        assertTrue(preloadablePreview(asset("next1", thumbnailUrl = THUMB), current))
        // 视频交给 VideoStage 播放，没有图片舞台可命中。
        assertFalse(preloadablePreview(asset("video", mediaType = "video", thumbnailUrl = THUMB), current))
        assertFalse(preloadablePreview(asset("other-target", target = OTHER, thumbnailUrl = THUMB), current))
        assertFalse(preloadablePreview(asset("blank", thumbnailUrl = ""), current))
        assertFalse(preloadablePreview(asset("current", thumbnailUrl = THUMB), current))
        // 超大/尺寸未知的原图仍值得预载 lg 预览：预览长边由服务端封顶 1024，不受原件准入限制。
        assertTrue(
            preloadablePreview(
                asset("huge", bytes = MAX_ORIGINAL_PRELOAD_BYTES + 1, width = 8000, height = 8000, thumbnailUrl = THUMB),
                current,
            ),
        )
        assertTrue(
            preloadablePreview(
                asset("unknown-size", bytes = 0, width = 0, height = 0, thumbnailUrl = THUMB),
                current,
            ),
        )
    }

    @Test
    fun `lg 预览清单保持近者优先并排除视频`() {
        val current = asset("current")
        val candidates = listOf(
            asset("next1", thumbnailUrl = THUMB),
            asset("prev1", thumbnailUrl = THUMB),
            asset("video", mediaType = "video", thumbnailUrl = THUMB),
            asset("next2", thumbnailUrl = ""),
            asset("next1", thumbnailUrl = THUMB),
        )
        assertEquals(
            listOf("next1", "prev1"),
            previewPreloadTargets(candidates, current).map { it.id },
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

    @Test
    fun `解码后超过 8MP 的原图不预载`() {
        val current = asset("current")
        // 12 MP：4 B/px ≈ 48 MB 位图，超过了 64 MiB 强缓存能长期容纳的单张量级。
        assertFalse(preloadableOriginal(asset("huge", width = 4000, height = 3000), current))
        // 恰好 8 MP 仍可预载（边界包含）。
        assertTrue(preloadableOriginal(asset("limit", width = 4000, height = 2000), current))
        // 尺寸未知（0×0）无法判断，按不可预载处理，避免把超大图当小图拉。
        assertFalse(preloadableOriginal(asset("unknown-size", width = 0, height = 0), current))
    }

    // ---------- 邻位下标（防「穿」到列表首项） ----------

    @Test
    fun `当前件不在已加载清单时邻位不动`() {
        // index=-1：照旧做 index+direction 后 getOrNull，左滑会取 source[0]，
        // 用户看到详情「穿」到列表第一张而不是相邻件。
        assertNull(adjacentAssetIndex(index = -1, direction = -1, size = 5))
        assertNull(adjacentAssetIndex(index = -1, direction = 1, size = 5))
        assertNull(adjacentAssetIndex(index = -1, direction = 1, size = 0))
        assertNull(adjacentAssetIndex(index = 5, direction = -1, size = 5))
    }

    @Test
    fun `邻位下标首尾边界不越界且合法邻居照常`() {
        // 第一张右滑 / 最后一张左滑：保持原有 getOrNull 边界行为，不环绕。
        assertNull(adjacentAssetIndex(index = 0, direction = -1, size = 5))
        assertNull(adjacentAssetIndex(index = 4, direction = 1, size = 5))
        assertNull(adjacentAssetIndex(index = 0, direction = -1, size = 1))
        assertNull(adjacentAssetIndex(index = 0, direction = 1, size = 1))
        // 合法邻居原样返回：中间双向与首尾向内。
        assertEquals(1, adjacentAssetIndex(index = 0, direction = 1, size = 5))
        assertEquals(3, adjacentAssetIndex(index = 4, direction = -1, size = 5))
        assertEquals(3, adjacentAssetIndex(index = 2, direction = 1, size = 5))
        assertEquals(1, adjacentAssetIndex(index = 2, direction = -1, size = 5))
    }

    // ---------- 详情首帧 lg 预览 ----------

    @Test
    fun `空或非法缩略图地址不构建预览`() {
        assertNull(detailPreviewUrl(""))
        assertNull(detailPreviewUrl("   "))
        assertNull(detailPreviewUrl("/api/v1/assets/a/thumbnail?version=7"))
        assertNull(detailPreviewUrl("not a url"))
    }

    @Test
    fun `预览地址固定取 lg 且保留版本参数`() {
        val url = detailPreviewUrl("http://example.test/api/v1/assets/a/thumbnail?version=7")!!.toHttpUrl()
        assertEquals(listOf("lg"), url.queryParameterValues("size"))
        assertEquals("7", url.queryParameter("version"))
        // 网格默认 md、小图 sm 都被替换成同一个 lg，不追加第二个 size。
        for (existing in listOf("md", "sm")) {
            val replaced = detailPreviewUrl(
                "http://example.test/api/v1/assets/a/thumbnail?version=7&size=$existing",
            )!!.toHttpUrl()
            assertEquals(listOf("lg"), replaced.queryParameterValues("size"))
            assertEquals("7", replaced.queryParameter("version"))
        }
    }

    @Test
    fun `重复的 size 参数收敛为一个 lg`() {
        val url = detailPreviewUrl(
            "http://example.test/api/v1/assets/a/thumbnail?version=7&size=md&size=sm",
        )!!.toHttpUrl()
        assertEquals(listOf("lg"), url.queryParameterValues("size"))
    }

    @Test
    fun `预览内存键含实例地址实例身份资产与内容版本_不复用网格或原图槽`() {
        val subject = asset("a", version = 7, thumbnailUrl = "http://example.test/api/v1/assets/a/thumbnail?version=7")
        assertEquals("http://example.test:instance:a:7:detail-preview", detailPreviewMemoryKey(subject))
        assertNotEquals(originalImageMemoryKey(subject), detailPreviewMemoryKey(subject))
        // 内容版本进入键：同一资产换内容后不能命中上一版的预览位图。
        assertNotEquals(
            detailPreviewMemoryKey(subject),
            detailPreviewMemoryKey(subject.copy(version = 8)),
        )
    }

    private fun asset(
        id: String,
        mediaType: String = "image",
        bytes: Long = 1024L,
        version: Long = 3,
        target: Session = SESSION,
        url: String = "http://example.test/$id",
        width: Int = 16,
        height: Int = 9,
        thumbnailUrl: String = "",
    ) = Asset(
        id = id, libraryId = "lib", target = target, name = id, originalName = id,
        relativePath = "$id.jpg", mediaType = mediaType, version = version, revision = 1, tagsRevision = 1,
        bytes = bytes, width = width, height = height, durationMs = null, authorId = null, authorIds = emptyList(),
        authorsRevision = 1, authorName = null, tags = emptyList(), favorite = false, liked = false,
        likeCount = 0, positionMs = 0, positionVersion = 0, note = "", sourceIds = emptyList(),
        characterIds = emptyList(), acquisitionSources = emptyList(), collectionId = null, page = null,
        contentUrl = url, thumbnailUrl = thumbnailUrl, addedAt = "2025-01-01T00:00:00Z",
    )

    private companion object {
        private const val THUMB = "http://example.test/api/v1/assets/a/thumbnail?version=7&size=md"
        private val SESSION = Session(baseUrl = "http://example.test", instanceId = "instance", label = "test")
        private val OTHER = Session(baseUrl = "http://other.test", instanceId = "instance", label = "other")
    }
}
