package media.qimeng.next.screen

import media.qimeng.next.model.Asset
import media.qimeng.next.model.Session
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * 视口窗口切片（[GalleryTileLayout]）的纯逻辑锁定：
 * - 槽位下标必须把组头算进去，与 MediaBrowser 的 LazyVerticalGrid 项序一致；
 * - 切片只解出请求范围（O(log 节数 + 窗口长度)），`base + local` 还原全局下标；
 * - 索引版播种/前瞻与旧的扁平清单版逐窗口等价，且跳过组头/空缩略图/动图。
 */
class GalleryTileLayoutTest {

    private val sections = listOf(
        GallerySection("d1", "今天  3 项", listOf(asset("a"), asset("b"), asset("c"))),
        GallerySection("d2", "昨天  2 项", listOf(asset("d"), asset("e"))),
        GallerySection("d3", null, listOf(asset("f"), asset("g"))),
    )

    /** 旧实现（`remember(sections) { buildList { ... } }`）产出的扁平槽位表，作为等价基准。 */
    private val flat: List<Asset?> = buildList {
        for (section in sections) {
            if (section.header != null) add(null)
            addAll(section.items)
        }
    }

    @Test
    fun `槽位表含组头且与扁平清单逐项一致`() {
        val layout = GalleryTileLayout(sections)
        assertEquals(flat.size, layout.size)
        for (index in flat.indices) {
            assertEquals("槽位 $index", flat[index]?.id, layout.assetAt(index)?.id)
        }
        assertNull(layout.assetAt(-1))
        assertNull(layout.assetAt(flat.size))
    }

    @Test
    fun `切片只解出请求范围并保留全局下标`() {
        val layout = GalleryTileLayout(sections)
        // [4,8) 从 d2 的组头开始、跨到 d3 的第二个条目。
        val slice = layout.slice(4, 8)
        assertEquals(TileSlice(4, flat.subList(4, 8)), slice)
        // 超出上界夹取到 size，起始下标即 base。
        assertEquals(TileSlice(flat.size, emptyList<Asset?>()), layout.slice(20, 30))
        // 下界负值夹取到 0。
        assertEquals(TileSlice(0, flat.subList(0, 2)), layout.slice(-5, 2))
        // 空范围不分配。
        assertEquals(TileSlice(3, emptyList<Asset?>()), layout.slice(3, 3))
    }

    @Test
    fun `索引版播种与扁平清单版逐窗口等价`() {
        val layout = GalleryTileLayout(sections)
        for (first in -1..flat.size + 1) {
            for (last in first..flat.size + 1) {
                for (limit in listOf(0, 1, 4, SEED_TILE_LIMIT_FOR_TEST)) {
                    assertEquals(
                        "first=$first last=$last limit=$limit",
                        seedTiles(flat, first, last, limit),
                        seedTiles(layout, first, last, limit),
                    )
                }
            }
        }
    }

    @Test
    fun `索引版前瞻与扁平清单版逐窗口等价`() {
        val layout = GalleryTileLayout(sections)
        for (first in -1..flat.size + 1) {
            for (last in first..flat.size + 1) {
                for (limit in listOf(0, 1, 4, 16)) {
                    for (forward in listOf(true, false)) {
                        assertEquals(
                            "first=$first last=$last limit=$limit forward=$forward",
                            lookaheadTiles(flat, first, last + 1, limit, forward),
                            lookaheadTiles(layout, first, last + 1, limit, forward),
                        )
                    }
                }
            }
        }
    }

    @Test
    fun `空清单与空节不崩`() {
        val empty = GalleryTileLayout(emptyList())
        assertEquals(0, empty.size)
        assertNull(empty.assetAt(0))
        assertNull(empty.firstAsset())
        assertEquals(emptyList<PrefetchItem>(), seedTiles(empty, 0, 5, 10))
        assertEquals(emptyList<PrefetchItem>(), lookaheadTiles(empty, 0, 1, 10, true))

        val withEmptySections = listOf(
            GallerySection("x", null, emptyList()),
            GallerySection("y", "有  1 项", listOf(asset("only"))),
            GallerySection("z", null, emptyList()),
        )
        val layout = GalleryTileLayout(withEmptySections)
        // 只有中间一节贡献槽位：组头 + 1 条。
        assertEquals(2, layout.size)
        assertNull(layout.assetAt(0))
        assertEquals("only", layout.assetAt(1)?.id)
        assertEquals("only", layout.firstAsset()?.id)
        assertEquals(listOf(null, withEmptySections[1].items.single()), layout.slice(0, 2).tiles)
        assertEquals(listOf(PrefetchItem(withEmptySections[1].items.single(), 1, seed = true)), seedTiles(layout, 0, 1, 10))
    }

    @Test
    fun `组头空缩略图与动图仍被跳过`() {
        val mixed = listOf(
            GallerySection(
                "d1", "今天  4 项",
                listOf(
                    asset("keep"),
                    asset("animated", mediaType = "animated_image"),
                    asset("no-thumb", thumb = ""),
                    asset("also-keep"),
                ),
            ),
        )
        val layout = GalleryTileLayout(mixed)
        // 组头占 0 号槽；a(1) 保留、动图(2)/空缩略图(3) 跳过、b(4) 保留。
        assertEquals(listOf("keep", "also-keep"), seedTiles(layout, 0, 4, 10).map { it.asset.id })
        assertEquals(listOf(1, 4), seedTiles(layout, 0, 4, 10).map { it.index })
        assertEquals(listOf("keep", "also-keep"), lookaheadTiles(layout, 0, 1, 10, true).map { it.asset.id })
        assertEquals(listOf("also-keep", "keep"), lookaheadTiles(layout, 5, 5, 10, false).map { it.asset.id })
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
        private const val SEED_TILE_LIMIT_FOR_TEST = 32
        private val SESSION = Session(baseUrl = "http://example.test", instanceId = "instance", label = "test")
    }
}
