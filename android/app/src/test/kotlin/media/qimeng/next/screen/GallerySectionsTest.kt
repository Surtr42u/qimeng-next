package media.qimeng.next.screen

import media.qimeng.next.model.Asset
import media.qimeng.next.model.AssetGroup
import media.qimeng.next.model.DateCount
import media.qimeng.next.model.FacetChoice
import media.qimeng.next.model.Session
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 日期分组的排序与组头计数（旧版 DateGrouping 口径）。
 *
 * 断言只依赖时间先后与 ISO 字符串的字典序，不依赖运行机器的时区：同一时刻在任意固定偏移下
 * 落入的日键保持单调，所以"键降序"等价于"组按最晚时间降序"。
 */
class GallerySectionsTest {
    private val now = 1_767_000_000_000L // 固定时钟：标签不随运行日变化

    @Test fun dateGroupsSortByLatestTimestampNotFirstAppearance() {
        val sections = gallerySections(
            items = listOf(
                asset("a", fileDate = "2026-01-01T00:00:00Z"),
                asset("c", fileDate = "2026-01-05T00:00:00Z"),
                asset("b", fileDate = "2026-01-03T00:00:00Z"),
            ),
            mode = "date", options = emptyList(), dates = emptyList(), unknown = null, nowMillis = now,
        )
        val keys = sections.map { it.key }
        assertEquals(keys.sortedDescending(), keys)
        assertEquals(listOf("c", "b", "a"), sections.flatMap { it.items }.map { it.id })
    }

    @Test fun unknownDateGroupIsAlwaysLast() {
        val sections = gallerySections(
            items = listOf(
                asset("undated"),
                asset("dated", fileDate = "2026-01-05T00:00:00Z"),
            ),
            mode = "date", options = emptyList(), dates = emptyList(), unknown = null, nowMillis = now,
        )
        assertEquals("dated", sections.first().items.single().id)
        assertEquals("", sections.last().key)
        assertTrue(sections.last().header.orEmpty().startsWith("未知日期"))
    }

    @Test fun headerFallsBackToLoadedCountWhenServerBucketsAreMissing() {
        val sections = gallerySections(
            items = listOf(
                asset("a", fileDate = "2026-01-05T00:00:00Z"),
                asset("b", fileDate = "2026-01-05T01:00:00Z"),
            ),
            mode = "date", options = emptyList(), dates = emptyList(), unknown = null, nowMillis = now,
        )
        // 两组都在 01-05：一个组两条，组头必须给出数字而不是只有标签。
        assertEquals(1, sections.size)
        assertTrue(sections.single().header.orEmpty().endsWith("2 项"))
    }

    @Test fun headerUsesServerBucketWhenPresent() {
        val probe = gallerySections(
            items = listOf(asset("a", fileDate = "2026-01-05T00:00:00Z")),
            mode = "date", options = emptyList(), dates = emptyList(), unknown = null, nowMillis = now,
        ).single()
        val sections = gallerySections(
            items = listOf(asset("a", fileDate = "2026-01-05T00:00:00Z")),
            mode = "date", options = emptyList(), dates = listOf(DateCount(probe.key, 42)), unknown = null, nowMillis = now,
        )
        assertTrue(sections.single().header.orEmpty().endsWith("42 项"))
    }

    @Test fun workGroupsPutOtherLastAndCountEveryGroup() {
        val sections = gallerySections(
            items = listOf(
                asset("a"), // 无作品归属 → 其他
                asset("b", works = listOf(AssetGroup("w1", "作品一"))),
            ),
            mode = "works",
            options = listOf(FacetChoice(key = "w1", kind = "work", id = "w1", name = "作品一", count = 7)),
            dates = emptyList(), unknown = null, nowMillis = now,
        )
        assertEquals("w1", sections.first().key)
        assertTrue(sections.first().header.orEmpty().endsWith("7 项"))
        // 无作品归属那一组的 key 为空串、组名「其他」，恒末位。
        assertEquals("", sections.last().key)
        assertTrue(sections.last().header.orEmpty().startsWith("其他"))
        assertTrue(sections.last().header.orEmpty().endsWith("1 项"))
    }

    @Test fun dayLabelsUseTheProvidedClockSnapshot() {
        val today = 1_767_000_000_000L
        val items = listOf(asset("a", fileDate = java.time.Instant.ofEpochMilli(today).toString()))
        val first = gallerySections(items, "date", emptyList(), emptyList(), null, today)
        val second = gallerySections(items, "date", emptyList(), emptyList(), null, today)
        assertEquals(first.single().header, second.single().header)
        assertTrue(first.single().header.orEmpty().startsWith("今天"))
    }

    private fun asset(
        id: String,
        fileDate: String? = null,
        works: List<AssetGroup> = emptyList(),
    ) = Asset(
        id = id, libraryId = "lib", target = SESSION, name = id, originalName = id,
        relativePath = "$id.jpg", mediaType = "image", version = 1, revision = 1, tagsRevision = 1,
        bytes = 1, width = 16, height = 9, durationMs = null, authorId = null, authorIds = emptyList(),
        authorsRevision = 1, authorName = null, tags = emptyList(), favorite = false, liked = false,
        likeCount = 0, positionMs = 0, positionVersion = 0, note = "", sourceIds = emptyList(),
        characterIds = emptyList(), acquisitionSources = emptyList(), collectionId = null, page = null,
        contentUrl = "http://example.test/$id", thumbnailUrl = "", addedAt = "2025-01-01T00:00:00Z",
        fileDate = fileDate, workGroups = works,
    )

    private companion object {
        private val SESSION = Session(baseUrl = "http://example.test", instanceId = "instance", label = "test")
    }
}
