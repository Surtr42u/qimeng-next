package media.qimeng.next.screen

import media.qimeng.next.model.*
import java.time.LocalDate
import java.util.TimeZone

internal data class GallerySection(val key: String, val header: String?, val items: List<Asset>)

private const val MILLIS_PER_DAY = 86_400_000L
private val WEEKDAY_LABELS = listOf("周一", "周二", "周三", "周四", "周五", "周六", "周日")

/**
 * Arrange visible rows using Go's facet identities, dates and full-result counts.
 *
 * [nowMillis] is the caller's single per-screen clock snapshot: the same value must be used for
 * every recomputation of one browser, or the day labels ("今天"/weekday/date) can flip mid-session
 * when the wall clock crosses a boundary. Defaults to now for callers with no screen lifetime.
 */
internal fun gallerySections(
    items: List<Asset>,
    mode: String?,
    options: List<FacetChoice>,
    dates: List<DateCount>,
    unknown: Long?,
    nowMillis: Long = System.currentTimeMillis(),
): List<GallerySection> {
    if (mode == null) return listOf(GallerySection("", null, items))
    if (mode == "works" || mode == "roles") {
        val counts = options.associate { it.key to it.count }
        // 「其他」（无作品/角色归属）恒末位，与旧版 DateGrouping 同日口径；其余保持服务端给出的组序。
        return items.groupBy { asset ->
            (if (mode == "works") asset.workGroups else asset.roleGroups).firstOrNull() ?: AssetGroup("", "其他")
        }.entries.sortedBy { it.key.name == "其他" }
            .map { (group, assets) -> GallerySection(group.key, header(group.name, counts[group.key], assets.size), assets) }
    }
    // One clock/zone snapshot per call: day keys stay integer-only, matching the server's
    // fixed-offset buckets and the old app's Math.floorDiv approach.
    val offsetMillis = (TimeZone.getDefault().getOffset(nowMillis) / 1000).toLong() * 1000
    val today = Math.floorDiv(nowMillis + offsetMillis, MILLIS_PER_DAY)
    val counts = dates.associate { it.date to it.count }
    fun stamp(asset: Asset): Long? = when (mode) {
        "history" -> asset.lastViewedAtMillis
        "favorite" -> asset.favoritedAtMillis
        else -> asset.fileDateMillis
    }
    val groups = items.groupBy { asset -> stamp(asset)?.let { Math.floorDiv(it + offsetMillis, MILLIS_PER_DAY) } }
    // 旧版 DateGrouping 按组内最大时间降序、未知日期恒末位。首现顺序只有在服务端按同一时间戳
    // 排序时才等价；面板改按浏览/收藏/大小排序后，就地分组会把旧日期组顶到最上面。
    return groups.entries.sortedWith(
        compareBy<Map.Entry<Long?, List<Asset>>> { it.key == null }
            .thenByDescending { entry -> entry.value.maxOfOrNull { stamp(it) ?: Long.MIN_VALUE } ?: Long.MIN_VALUE },
    ).map { (day, assets) ->
        if (day == null) return@map GallerySection("", header("未知日期", unknown, assets.size), assets)
        val date = LocalDate.ofEpochDay(day)
        val key = date.toString()
        val label = when (today - day) {
            0L -> "今天"
            1L -> "昨天"
            in 2L..6L -> WEEKDAY_LABELS[date.dayOfWeek.value - 1]
            else -> key
        }
        GallerySection(key, header(label, counts[key], assets.size), assets)
    }
}

/**
 * 组头永远带数字：服务端分桶缺失时回退本组已加载条数（旧版 DateGrouping.kt:90 同口径）。
 * 回退值不可能是 0——一个组出现的前提就是它至少有一条已加载条目，因此不违反
 * `CountSeeds` 的"不得凭空造 0"规则。
 */
private fun header(label: String, count: Long?, loaded: Int) = "$label  ${count ?: loaded} 项"
