package media.qimeng.next.screen

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.*
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.chart.QimengTrendLineChart
import media.qimeng.next.ui.chart.QimengTrendSeries
import media.qimeng.next.ui.icon.BackIcon
import media.qimeng.next.ui.theme.QimengStatsSeriesColors
import media.qimeng.next.ui.theme.qimengStatsSeriesColors
import media.qimeng.next.ui.theme.softDaylightShadow

@Composable
fun StatsDetailScreen(model: LibraryViewModel, mode: String, range: String) {
    val state by model.analytics.collectAsStateWithLifecycle()
    val seriesColors = qimengStatsSeriesColors()
    LaunchedEffect(range) { if (state.data?.overview?.range != range) model.loadAnalytics(range) }
    val title = when (mode) { "trends" -> "类型浏览趋势"; "distribution" -> "分布统计"; else -> "常看作者与标签" }
    Column(Modifier.fillMaxSize()) {
        StatsDetailTopBar(if (mode == "distribution") title else "$title · ${statsRangeSuffix(range)}", model::back)
        if (state.loading) Text("加载中…", Modifier.padding(horizontal = 16.dp, vertical = 16.dp), style = MaterialTheme.typography.bodyMedium)
        state.error?.let { ErrorNotice(it) { model.loadAnalytics(range) } }
        state.data?.takeIf { it.overview.range == range }?.let { report ->
            val o = report.overview
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                when (mode) {
                    "trends" -> {
                        item { DetailTrendCard("类型浏览趋势", typeDimensions(seriesColors), report.trends) }
                        item { DetailTrendCard("来源浏览趋势", sourceDimensions(seriesColors), report.trends) }
                    }
                    "distribution" -> {
                        val image = o.mediaTypes.first { it.key == "image" }
                        val video = o.mediaTypes.first { it.key == "video" }
                        val animated = o.mediaTypes.first { it.key == "animated_image" }
                        val regular = o.sources.first { it.key == "regular" }
                        val cos = o.sources.first { it.key == "cos" }
                        item { StatSummaryGrid(listOf(
                            "图片" to "${image.assets} (${share(image.assets, o.inventory.assets)}%)",
                            "视频" to "${video.assets} (${share(video.assets, o.inventory.assets)}%)",
                            "动图" to "${animated.assets} (${share(animated.assets, o.inventory.assets)}%)",
                            "常规 / COS" to "${regular.assets} / ${cos.assets}",
                            "图片总大小" to formatSizeDetail(image.bytes), "视频总大小" to formatSizeDetail(video.bytes),
                            "总占用" to formatSizeDetail(o.inventory.bytes), "窗口浏览" to formatCountDetail(o.totals.views))) }
                        item { StatInsights(buildList {
                            add("近${if (range == "all") "全部时间" else if (range == "30d") "30天" else "7天"}图片被浏览 ${image.views} 次，视频 ${video.views} 次，动图 ${animated.views} 次")
                            if (cos.assets > 0) add("COS 文件 ${cos.assets} 个（${share(cos.assets, o.inventory.assets)}%），窗口浏览 ${cos.views} 次")
                        }) }
                        item { DistributionCard("类型分布对比", "库存数量/大小 vs 窗口浏览", listOf(image, video, animated)) }
                        item { DistributionCard("来源分布对比", "常规 vs COS", listOf(regular, cos)) }
                    }
                    else -> {
                        val authors = report.top.getValue("authors")
                        val tags = report.top.getValue("tags")
                        item { StatSummaryGrid(listOf("作者" to formatCountDetail(o.viewedAuthors), "标签" to formatCountDetail(o.viewedTags),
                            "浏览总次数" to formatCountDetail(o.totals.views), "Top 作者" to (authors.firstOrNull()?.name ?: "—"))) }
                        item { StatInsights(buildList {
                            authors.firstOrNull()?.let { add("最常看作者「${it.name}」，浏览 ${it.views} 次") }
                            tags.firstOrNull()?.let { add("最常看标签「${it.name}」，浏览 ${it.views} 次") }
                        }) }
                        item { InterestRankingCard("常看作者", "按浏览聚合 · Top 15", "作者", authors) { model.openAuthor(it.id) } }
                        item { InterestRankingCard("常看标签", "按浏览聚合 · Top 10", "标签", tags) { model.openTagSearch(it.name) } }
                    }
                }
            }
        }
    }
}

internal fun statsRangeSuffix(range: String) = when (range) { "30d" -> "近30天"; "all" -> "全部"; else -> "近7天" }
private fun share(value: Long, total: Long) = if (total == 0L) 0 else (value.toDouble() * 100 / total).toInt()

@Composable
internal fun StatsDetailTopBar(title: String, onBack: () -> Unit) {
    Row(Modifier.fillMaxWidth().height(56.dp).padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(32.dp).clickable(onClick = onBack), contentAlignment = Alignment.Center) {
            Icon(BackIcon, "返回", tint = MaterialTheme.colorScheme.onBackground)
        }
        Text(title, Modifier.padding(start = 8.dp), fontSize = 18.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.onBackground)
    }
}

@Composable
internal fun DetailStatCard(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    Surface(modifier.softDaylightShadow(RoundedCornerShape(20.dp)), color = MaterialTheme.colorScheme.surface,
        contentColor = MaterialTheme.colorScheme.onSurface, shape = RoundedCornerShape(20.dp)) {
        Column(Modifier.padding(12.dp), content = content)
    }
}

@Composable
internal fun StatSummaryGrid(cells: List<Pair<String, String>>) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        cells.chunked(2).forEach { row -> Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            row.forEach { (label, value) -> DetailStatCard(Modifier.weight(1f)) {
                Text(value, fontSize = 18.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.primary)
                Text(label, fontSize = 11.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
            } }
            if (row.size == 1) Spacer(Modifier.weight(1f))
        } }
    }
}

@Composable
internal fun StatInsights(lines: List<String>) {
    if (lines.isEmpty()) return
    DetailStatCard(Modifier.fillMaxWidth()) {
        StatsTitle("数据洞察")
        lines.forEach { Text("· $it", Modifier.padding(top = 6.dp), fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

private data class StatDimension(val key: String, val label: String, val color: Color)

/** 系列色由当前主题色板推导（浅/深自动切换），分类、顺序与文案不变。 */
private fun typeDimensions(colors: QimengStatsSeriesColors) = listOf(
    StatDimension("image", "图片", colors.image),
    StatDimension("video", "视频", colors.video),
    StatDimension("animated_image", "动图", colors.animated),
)

private fun sourceDimensions(colors: QimengStatsSeriesColors) = listOf(
    StatDimension("regular", "常规", colors.regular),
    StatDimension("cos", "COS", colors.cos),
)

@Composable
private fun DetailTrendCard(title: String, dimensions: List<StatDimension>, points: List<TrendPoint>) {
    var selected by rememberSaveable(title) { mutableStateOf(dimensions.map { it.key }) }
    val active = dimensions.filter { it.key in selected }
    DetailStatCard(Modifier.fillMaxWidth()) {
        StatsTitle(title)
        Row(Modifier.fillMaxWidth().padding(top = 8.dp).height(34.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            dimensions.forEach { dimension ->
                val enabled = dimension.key in selected
                Box(Modifier.weight(1f).fillMaxHeight().clip(RoundedCornerShape(100.dp))
                    .background(if (enabled) MaterialTheme.colorScheme.primary else Color.Transparent).clickable {
                        if (!enabled) selected = selected + dimension.key else if (selected.size > 1) selected = selected - dimension.key
                    }, contentAlignment = Alignment.Center) {
                    Text(dimension.label, fontSize = 12.sp, color = if (enabled) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
        Row(Modifier.fillMaxWidth().padding(top = 8.dp)) { active.forEach { d ->
            Row(Modifier.weight(1f), horizontalArrangement = Arrangement.Center, verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.size(8.dp).background(d.color)); Spacer(Modifier.width(4.dp))
                Text(d.label, fontSize = 10.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        } }
        if (points.isEmpty()) Text("暂无趋势数据", Modifier.padding(top = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
        else QimengTrendLineChart(active.map { dimension -> QimengTrendSeries(points.map { point ->
            (point.mediaTypes + point.sources).first { it.key == dimension.key }.views }, dimension.label) },
            active.map { it.color }, points.map { it.label }, Modifier.fillMaxWidth().padding(top = 8.dp).height(240.dp),
            chartTitle = title)
    }
}

@Composable
private fun InterestRankingCard(title: String, subtitle: String, kind: String, rows: List<ActivityRank>, onOpen: (ActivityRank) -> Unit) {
    DetailStatCard(Modifier.fillMaxWidth()) {
        StatsTitle(title)
        Text(subtitle, Modifier.padding(top = 2.dp), fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            if (rows.isEmpty()) InterestRankRow(null, "暂无数据", null, null, 0, null)
            rows.forEachIndexed { i, row -> InterestRankRow(i + 1, row.name, kind, "${row.views} 次", rankProgressPercent(row.views, rows.first().views)) { onOpen(row) } }
        }
    }
}

@Composable
private fun InterestRankRow(rank: Int?, title: String, subtitle: String?, value: String?, percent: Int, onClick: (() -> Unit)?) {
    Surface(Modifier.fillMaxWidth().softDaylightShadow(RoundedCornerShape(20.dp)).then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier),
        color = MaterialTheme.colorScheme.surface, contentColor = MaterialTheme.colorScheme.onSurface, shape = RoundedCornerShape(20.dp)) {
        Row(Modifier.padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(rank?.toString() ?: "—", Modifier.width(28.dp), fontSize = 16.sp, fontWeight = FontWeight.Bold, textAlign = TextAlign.Center,
                color = if (rank != null && rank <= 3) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant)
            Column(Modifier.weight(1f).padding(start = 10.dp)) {
                Text(title, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                subtitle?.let { Text(it, Modifier.padding(top = 2.dp), fontSize = 11.sp, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                StatMiniProgress(percent, Modifier.padding(top = 6.dp))
            }
            value?.let { Text(it, Modifier.padding(start = 8.dp), fontSize = 14.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.primary) }
        }
    }
}

@Composable
private fun DistributionCard(title: String, subtitle: String, rows: List<ActivityDimension>) {
    DetailStatCard(Modifier.fillMaxWidth()) {
        StatsTitle(title)
        Text(subtitle, Modifier.padding(top = 2.dp), fontSize = 11.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
        rows.forEach { row -> Column(Modifier.padding(top = 12.dp)) {
            Text(typeName(row.key), fontSize = 13.sp, fontWeight = FontWeight.Bold)
            Row(Modifier.fillMaxWidth().padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                listOf(Triple("数量", row.assets.toString(), rankProgressPercent(row.assets, rows.maxOf { it.assets })),
                    Triple("大小", formatSizeDetail(row.bytes), rankProgressPercent(row.bytes, rows.maxOf { it.bytes })),
                    Triple("浏览", row.views.toString(), rankProgressPercent(row.views, rows.maxOf { it.views }))).forEach { (name, value, percent) ->
                    Column(Modifier.weight(1f)) {
                        Text(name, fontSize = 10.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        StatMiniProgress(percent, Modifier.padding(top = 4.dp))
                        Text(value, Modifier.padding(top = 2.dp), fontSize = 12.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.primary)
                    }
                }
            }
        } }
    }
}

@Composable
private fun StatMiniProgress(percent: Int, modifier: Modifier = Modifier) {
    Box(modifier.fillMaxWidth().height(3.dp).clip(RoundedCornerShape(2.dp)).background(MaterialTheme.colorScheme.surfaceVariant)) {
        if (percent > 0) Box(Modifier.fillMaxWidth(percent / 100f).height(3.dp).clip(RoundedCornerShape(2.dp)).background(MaterialTheme.colorScheme.secondary))
    }
}
