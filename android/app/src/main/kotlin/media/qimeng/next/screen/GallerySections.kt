package media.qimeng.next.screen

import media.qimeng.next.model.*
import java.time.LocalDate
import java.util.TimeZone

internal data class GallerySection(val key: String, val header: String?, val items: List<Asset>)

private const val MILLIS_PER_DAY = 86_400_000L
private val WEEKDAY_LABELS = listOf("周一", "周二", "周三", "周四", "周五", "周六", "周日")

/** Arrange visible rows using Go's facet identities, dates and full-result counts. */
internal fun gallerySections(items: List<Asset>, mode: String?, options: List<FacetChoice>, dates: List<DateCount>, unknown: Long?): List<GallerySection> {
    if (mode == null) return listOf(GallerySection("", null, items))
    if (mode == "works" || mode == "roles") {
        val counts = options.associate { it.key to it.count }
        return items.groupBy { asset ->
            (if (mode == "works") asset.workGroups else asset.roleGroups).firstOrNull() ?: AssetGroup("", "其他")
        }.map { (group, assets) -> GallerySection(group.key, header(group.name, counts[group.key]), assets) }
    }
    // One clock/zone snapshot per call: day keys stay integer-only, matching the server's
    // fixed-offset buckets and the old app's Math.floorDiv approach.
    val nowMillis = System.currentTimeMillis()
    val offsetMillis = (TimeZone.getDefault().getOffset(nowMillis) / 1000).toLong() * 1000
    val today = Math.floorDiv(nowMillis + offsetMillis, MILLIS_PER_DAY)
    val counts = dates.associate { it.date to it.count }
    val groups = items.groupBy { asset ->
        val timestamp = when (mode) {
            "history" -> asset.lastViewedAtMillis
            "favorite" -> asset.favoritedAtMillis
            else -> asset.fileDateMillis
        }
        timestamp?.let { Math.floorDiv(it + offsetMillis, MILLIS_PER_DAY) }
    }
    return groups.entries.sortedBy { it.key == null }.map { (day, assets) ->
        if (day == null) return@map GallerySection("", header("未知日期", unknown), assets)
        val date = LocalDate.ofEpochDay(day)
        val key = date.toString()
        val label = when (today - day) {
            0L -> "今天"
            1L -> "昨天"
            in 2L..6L -> WEEKDAY_LABELS[date.dayOfWeek.value - 1]
            else -> key
        }
        GallerySection(key, header(label, counts[key]), assets)
    }
}

private fun header(label: String, count: Long?) = if (count == null) label else "$label  $count 项"
