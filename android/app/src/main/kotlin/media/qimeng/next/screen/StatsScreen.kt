package media.qimeng.next.screen

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.AnalyticsReport
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.chart.QimengTrendLineChart
import media.qimeng.next.ui.chart.QimengTrendSeries
import media.qimeng.next.ui.component.QimengSegmentedControl
import media.qimeng.next.ui.theme.softDaylightBackground
import media.qimeng.next.ui.theme.softDaylightShadow

/** Original statistics layout; every displayed total and mixed ranking comes from the server. */
@Composable
fun StatisticsScreen(model: LibraryViewModel) {
    var range by rememberSaveable { mutableStateOf("7d") }
    val state by model.analytics.collectAsStateWithLifecycle()
    // 进程恢复会把已访问 Tab 一起上树；隐藏层不发起统计请求，首次可见时再补。
    val sceneActive = media.qimeng.next.screen.LocalSceneActive.current
    // 范围变化立即取；回前台按新鲜度窗口补读（见 ensureAnalytics）。窗口内重进标签页不重取，
    // 窗口过期时旧数字留在屏上直到新报告落地。
    LaunchedEffect(range, sceneActive) { if (sceneActive) model.ensureAnalytics(range) }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { if (sceneActive) model.ensureAnalytics(range) }
    val report = state.data?.takeIf { it.overview.range == range }
    val loading = state.error == null && report == null
    val scroll = androidx.compose.foundation.lazy.rememberLazyListState()
    LaunchedEffect(scroll) { media.qimeng.next.ui.TabScrollController.events.collect { if (it == 2) scroll.scrollToItem(0) } }
    LazyColumn(Modifier.fillMaxSize().softDaylightBackground().padding(horizontal = 16.dp), state = scroll,
        verticalArrangement = Arrangement.spacedBy(16.dp),
        contentPadding = PaddingValues(bottom = media.qimeng.next.ui.glass.TabDockDefaults.bottomClearance())) {
        item { Text("数据统计", style = MaterialTheme.typography.headlineSmall.copy(fontWeight = FontWeight.Bold)) }
        item { RangeSelector(range) { range = it } }
        state.error?.let { item { ErrorNotice(it) { model.loadAnalytics(range) } } }
        item { OverviewCards(report, loading) }
        item {
            StatsCard(onClick = { model.navigate("stats-detail:trends:$range") }) {
                StatsCardHeading("浏览趋势", "分类型趋势 ›")
                when {
                    loading -> Text("加载中…", style = MaterialTheme.typography.bodyMedium)
                    report?.trends?.isEmpty() == true -> Text("暂无趋势数据", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    report != null -> QimengTrendLineChart(listOf(QimengTrendSeries(report.trends.map { it.views })), listOf(MaterialTheme.colorScheme.primary),
                        report.trends.map { it.label }, Modifier.fillMaxWidth().height(200.dp))
                }
            }
        }
        item {
            StatsCard(onClick = { model.navigate("stats-detail:distribution:$range") }, padding = PaddingValues(horizontal = 12.dp, vertical = 14.dp)) {
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        StatsTitle("分布统计")
                        Text("类型与来源的库存构成", fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    Text("查看详情 ›", style = MaterialTheme.typography.labelMedium.copy(fontWeight = FontWeight.Normal), color = MaterialTheme.colorScheme.primary)
                }
            }
        }
        item { StatsCard(onClick = { model.navigate("review:$range") }) { StatsTitle("浏览回顾") } }
        item {
            StatsCard(onClick = { model.navigate("stats-detail:interests:$range") }) {
                StatsCardHeading("常看作者与标签", "查看全部 ›")
                when {
                    loading -> CompactStatText("加载中…")
                    report?.interests?.isEmpty() == true -> CompactStatText("暂无数据")
                    report != null -> report.interests.forEach { entry ->
                        Row(Modifier.fillMaxWidth().clickable {
                            if (entry.kind == "author") model.openAuthor(entry.id) else model.openTagSearch(entry.name)
                        }, horizontalArrangement = Arrangement.SpaceBetween) {
                            Text(entry.name, Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium.copy(fontWeight = FontWeight.Bold), maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(entry.views.toDisplayText() + " 次", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.primary)
                        }
                    }
                }
            }
        }
        item { Spacer(Modifier.height(8.dp)) }
    }
}

@Composable
internal fun RangeSelector(selected: String, onSelected: (String) -> Unit) {
    val ranges = listOf("7d", "30d", "all")
    QimengSegmentedControl(listOf("7天", "30天", "全部"), ranges.indexOf(selected).coerceAtLeast(0), { onSelected(ranges[it]) })
}

@Composable
private fun OverviewCards(report: AnalyticsReport?, loading: Boolean) {
    val o = report?.overview
    val placeholder = if (loading) "加载中…" else "—"
    val values = listOf(
        "总浏览次数" to (o?.totals?.views?.let(::formatCountCompact) ?: placeholder),
        "总播放次数" to (o?.totals?.plays?.let(::formatCountCompact) ?: placeholder),
        "总浏览时长" to (o?.totals?.seconds?.let(::formatDurationSeconds) ?: placeholder),
        "总文件数" to (o?.inventory?.assets?.let(::formatCountCompact) ?: placeholder),
        "总占用空间" to (o?.inventory?.bytes?.let(::statsBytes) ?: placeholder),
        "平均浏览次数" to (o?.averageViews?.let(::trimTrailingZero) ?: placeholder))
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        values.chunked(3).forEach { row -> Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            row.forEach { (title, value) ->
                Surface(Modifier.weight(1f).softDaylightShadow(RoundedCornerShape(20.dp)),
                    color = MaterialTheme.colorScheme.surface, contentColor = MaterialTheme.colorScheme.onSurface, shape = RoundedCornerShape(20.dp)) {
                    Column(Modifier.padding(horizontal = 12.dp, vertical = 10.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                        Text(value, style = MaterialTheme.typography.titleMedium.copy(fontSize = 20.sp, fontWeight = FontWeight.Bold), color = MaterialTheme.colorScheme.primary)
                        Text(title, style = MaterialTheme.typography.labelSmall.copy(fontWeight = FontWeight.Normal), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        } }
    }
}

@Composable
internal fun StatsCard(modifier: Modifier = Modifier, onClick: (() -> Unit)? = null, padding: PaddingValues = PaddingValues(12.dp), content: @Composable ColumnScope.() -> Unit) {
    Surface(modifier.fillMaxWidth().softDaylightShadow(RoundedCornerShape(20.dp)).then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier),
        shape = RoundedCornerShape(20.dp), color = MaterialTheme.colorScheme.surface, contentColor = MaterialTheme.colorScheme.onSurface) {
        Column(Modifier.padding(padding), verticalArrangement = Arrangement.spacedBy(8.dp), content = content)
    }
}
@Composable internal fun StatsTitle(text: String) { Text(text, style = MaterialTheme.typography.titleMedium.copy(fontSize = 14.sp, fontWeight = FontWeight.Bold)) }
@Composable internal fun StatsCardHeading(title: String, link: String) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
        StatsTitle(title)
        Text(link, style = MaterialTheme.typography.labelMedium.copy(fontWeight = FontWeight.Normal), color = MaterialTheme.colorScheme.primary)
    }
}
@Composable private fun CompactStatText(text: String) { Text(text, fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant) }
private fun statsBytes(bytes: Long): String = when {
    bytes >= 1_073_741_824L -> String.format(java.util.Locale.US, "%.1fGB", bytes.toDouble() / 1_073_741_824L)
    bytes >= 1_048_576L -> String.format(java.util.Locale.US, "%.1fMB", bytes.toDouble() / 1_048_576L)
    else -> "${bytes}B"
}
