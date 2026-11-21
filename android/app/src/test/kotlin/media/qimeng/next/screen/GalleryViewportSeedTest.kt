package media.qimeng.next.screen

import media.qimeng.next.model.Asset
import media.qimeng.next.model.Session
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 可见窗口播种（搜索/换查询后首屏预热）的纯逻辑锁定：只取窗口内、顺序自上而下、
 * 跳过组头/空缩略图/动图，并按上限截断（余量留给随后入队的前瞻窗口）。
 */
class GalleryViewportSeedTest {

    @Test
    fun `只取可见窗口且自上而下`() {
        val tiles = listOf(null, asset("a"), asset("b"), asset("c"), asset("d"))
        val seeds = seedTiles(tiles, first = 0, last = 3, limit = 10)
        assertEquals(listOf("a", "b", "c"), seeds.map { it.asset.id })
        assertEquals(listOf(1, 2, 3), seeds.map { it.index })
        assertTrue("种子必须带 seed 标记，位置剪枝不能把它们清掉", seeds.all { it.seed })
    }

    @Test
    fun `按上限截断_给前瞻窗口留位置`() {
        val tiles = (1..10).map { asset("a$it") }
        val seeds = seedTiles(tiles, first = 0, last = 9, limit = 4)
        assertEquals(listOf("a1", "a2", "a3", "a4"), seeds.map { it.asset.id })
        assertEquals(emptyList<PrefetchItem>(), seedTiles(tiles, first = 0, last = 9, limit = 0))
    }

    @Test
    fun `跳过组头空缩略图与动图`() {
        val tiles = listOf(
            asset("keep"),
            asset("animated", mediaType = "animated_image"),
            asset("no-thumb", thumb = ""),
            asset("also-keep"),
        )
        assertEquals(listOf("keep", "also-keep"), seedTiles(tiles, 0, 3, 10).map { it.asset.id })
    }

    @Test
    fun `窗口下标越界与空窗口不崩`() {
        val tiles = listOf(null, asset("a"), asset("b"))
        assertEquals(emptyList<PrefetchItem>(), seedTiles(tiles, first = -1, last = -1, limit = 10))
        assertEquals(emptyList<PrefetchItem>(), seedTiles(tiles, first = 5, last = 9, limit = 10))
        assertEquals(emptyList<PrefetchItem>(), seedTiles(emptyList(), first = 0, last = 5, limit = 10))
        assertEquals(listOf("b"), seedTiles(tiles, first = 2, last = 99, limit = 10).map { it.asset.id })
    }

    private fun asset(id: String, mediaType: String = "image", thumb: String = "http://example.test/$id/thumb") = Asset(
        id = id, libraryId = "lib", target = SESSION, name = id, originalName = id,
        relativePath = "$id.jpg", mediaType = mediaType, version = 3, revision = 1, tagsRevision = 1,
        bytes = 1024, width = 16, height = 9, durationMs = null, authorId = null, authorIds = emptyList(),
        authorsRevision = 1, authorName = null, tags = emptyList(), favorite = false, liked = false,
        likeCount = 0, positionMs = 0, positionVersion = 0, note = "", sourceIds = emptyList(),
        characterIds = emptyList(), acquisitionSources = emptyList(), collectionId = null, page = null,
        contentUrl = "http://example.test/$id", thumbnailUrl = thumb, addedAt = "2025-01-01T00:00:00Z",
    )

    private companion object {
        private val SESSION = Session(baseUrl = "http://example.test", instanceId = "instance", label = "test")
    }
}
