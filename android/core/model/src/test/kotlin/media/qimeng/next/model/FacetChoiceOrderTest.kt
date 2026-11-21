package media.qimeng.next.model

import org.junit.Assert.*
import org.junit.Test

/**
 * 候选行「其他」置底的纯逻辑锁定（见 [withOtherBucketLast]）。
 *
 * 服务端只承诺候选按计数降序（`server/internal/service/asset_facets.go` 的 `facet_count`），
 * 协议不承诺兜底桶位置；旧版 Android `core/model/FacetOption.kt` 由客户端把「其他」恒置底，
 * 「全部」胶囊仍在最前。置底只改显示顺序：AlbumFilters 用同一个 `items` 列表渲染胶囊并按下标
 * 回查点击项，所以这里只断言顺序，不涉及选中态。
 */
class FacetChoiceOrderTest {

    @Test fun otherBucketGoesLastWhateverItsCount() {
        // 计数最高的「其他」也要置底：这里的顺序就是服务端计数降序的返回顺序。
        assertEquals(
            listOf("作品一", "作品二", "其他"),
            listOf(choice("其他", 900), choice("作品一", 50), choice("作品二", 10))
                .withOtherBucketLast().map { it.name },
        )
        // 计数最低、夹在中间时同样置底。
        assertEquals(
            listOf("作品一", "作品二", "其他"),
            listOf(choice("作品一", 50), choice("其他", 1), choice("作品二", 10))
                .withOtherBucketLast().map { it.name },
        )
        // 只按显示名精确匹配：「其他作品」是普通候选，不置底。
        assertEquals(
            listOf("其他作品", "作品一", "其他"),
            listOf(choice("其他作品", 80), choice("作品一", 50), choice("其他", 5))
                .withOtherBucketLast().map { it.name },
        )
    }

    @Test fun orderIsUnchangedWithoutOtherBucket() {
        val items = listOf(choice("作品一", 50), choice("作品二", 10), choice("作品三", 3))
        assertEquals(items, items.withOtherBucketLast())
    }

    @Test fun multipleOtherBucketsKeepTheirRelativeOrder() {
        val ordered = listOf(
            choice("其他", 90, key = "other-1"),
            choice("作品一", 50),
            choice("其他", 30, key = "other-2"),
            choice("其他", 1, key = "other-3"),
        ).withOtherBucketLast()
        // 非「其他」项原地不动，多个「其他」之间仍按传入顺序（稳定 partition）。
        assertEquals(listOf("作品一", "other-1", "other-2", "other-3"), ordered.map { it.key })
    }

    @Test fun emptyListStaysEmpty() {
        assertEquals(emptyList<FacetChoice>(), emptyList<FacetChoice>().withOtherBucketLast())
    }

    @Test fun allPillStaysFirst() {
        // 动态行：先拼「全部」再置底，结果必须是 全部 → 计数降序 → 其他。
        val all = FacetChoice(key = "all", kind = "works", id = "all", name = "全部", count = 1_000)
        val ordered = (listOf(all) + listOf(choice("其他", 700), choice("作品一", 5)))
            .withOtherBucketLast()
        assertEquals(listOf("全部", "作品一", "其他"), ordered.map { it.name })
    }

    private fun choice(name: String, count: Long, key: String = name) =
        FacetChoice(key = key, kind = "work", id = key, name = name, count = count)
}
