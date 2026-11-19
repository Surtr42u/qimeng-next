package media.qimeng.next.screen

import media.qimeng.next.model.*
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import java.time.temporal.ChronoUnit
import java.util.TimeZone

internal data class GallerySection(val key: String, val header: String?, val items: List<Asset>)

/** Arrange visible rows using Go's facet identities, dates and full-result counts. */
internal fun gallerySections(items: List<Asset>, mode: String?, options: List<FacetChoice>, dates: List<DateCount>, unknown: Long?): List<GallerySection> {
    if (mode == null) return listOf(GallerySection("", null, items))
    if (mode == "works" || mode == "roles") {
        val counts = options.associate { it.key to it.count }
        return items.groupBy { asset ->
            (if (mode == "works") asset.workGroups else asset.roleGroups).firstOrNull() ?: AssetGroup("", "其他")
        }.map { (group, assets) -> GallerySection(group.key, header(group.name, counts[group.key]), assets) }
    }
    val offset = ZoneOffset.ofTotalSeconds(TimeZone.getDefault().getOffset(System.currentTimeMillis()) / 1000)
    val today = LocalDate.now(offset)
    val counts = dates.associate { it.date to it.count }
    val groups = items.groupBy { asset ->
        val timestamp = when (mode) {
            "history" -> asset.lastViewedAt
            "favorite" -> asset.favoritedAt
            else -> asset.fileDate
        }
        timestamp?.let { Instant.parse(it).atOffset(offset).toLocalDate() }
    }
    return groups.entries.sortedBy { it.key == null }.map { (day, assets) ->
        val label = if (day == null) "未知日期" else when (ChronoUnit.DAYS.between(day, today)) {
            0L -> "今天"
            1L -> "昨天"
            in 2L..6L -> listOf("周一", "周二", "周三", "周四", "周五", "周六", "周日")[day.dayOfWeek.value - 1]
            else -> day.toString()
        }
        GallerySection(day?.toString().orEmpty(), header(label, if (day == null) unknown else counts[day.toString()]), assets)
    }
}

private fun header(label: String, count: Long?) = if (count == null) label else "$label  $count 项"
