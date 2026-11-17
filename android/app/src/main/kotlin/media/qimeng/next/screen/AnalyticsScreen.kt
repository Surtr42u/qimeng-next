package media.qimeng.next.screen

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.model.*
import media.qimeng.next.ui.ContentCard
import media.qimeng.next.ui.ErrorNotice
import kotlin.math.abs
import kotlin.math.roundToInt

@Composable
fun StatisticsScreen(model: LibraryViewModel) {
    var range by rememberSaveable { mutableStateOf("7d") }
    val state by model.analytics.collectAsStateWithLifecycle()
    LaunchedEffect(range) { if (state.data?.overview?.range != range) model.loadAnalytics(range) }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = 128.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        PageHeader("数据", actions = { IconButton(onClick = { model.loadAnalytics(range) }) { Icon(Icons.Outlined.Refresh, "刷新统计") } })
        RangeSelector(range) { range = it }
        if (state.loading) LinearProgressIndicator(Modifier.fillMaxWidth())
        state.error?.let { ErrorNotice(it) { model.loadAnalytics(range) } }
        state.data?.takeIf { it.overview.range == range }?.let { report ->
            val o = report.overview
            ContentCard(Modifier.padding(horizontal = 16.dp)) {
                Text("浏览概况", style = MaterialTheme.typography.titleMedium)
                Text("${o.totals.views} 次浏览", style = MaterialTheme.typography.headlineLarge)
                Text("停留 ${formatSeconds(o.totals.seconds)} · 播放 ${o.totals.plays} 次")
                Text("活跃 ${o.activeDays} 天 · 浏览过 ${o.viewedAssets} 件媒体")
                Text("${o.viewedAuthors} 位作者 · ${o.viewedTags} 个标签")
                o.averageViews?.let { Text("每件媒体平均 %.1f 次浏览".format(it), style = MaterialTheme.typography.bodySmall) }
            }
            ContentCard(Modifier.padding(horizontal = 16.dp)) {
                Text("浏览趋势", style = MaterialTheme.typography.titleMedium)
                TrendChart(report.trends)
            }
            ContentCard(Modifier.padding(horizontal = 16.dp)) {
                Text("媒体类型", style = MaterialTheme.typography.titleMedium)
                o.mediaTypes.forEach { value -> DimensionRow(value) }
            }
            if (o.sources.isNotEmpty()) ContentCard(Modifier.padding(horizontal = 16.dp)) {
                Text("来源", style = MaterialTheme.typography.titleMedium)
                o.sources.forEach { DimensionRow(it) }
            }
            ContentCard(Modifier.padding(horizontal = 16.dp).fillMaxWidth().clickable { model.navigate("review:$range") }) {
                Text("浏览回顾", style = MaterialTheme.typography.titleMedium)
            }
            ContentCard(Modifier.padding(horizontal = 16.dp)) {
                Text("当前影库库存", style = MaterialTheme.typography.titleMedium)
                Text("${o.inventory.assets} 件 · ${formatBytes(o.inventory.bytes)}")
                Text("收藏 ${o.inventory.favorites} · 作者 ${o.inventory.authors} · 回收站 ${o.inventory.trash}")
            }
        }
    }
}

@Composable
private fun RangeSelector(selected: String, onSelected: (String) -> Unit) {
    Row(Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        listOf("7d" to "7 天", "30d" to "30 天", "90d" to "90 天", "week" to "12 周", "month" to "24 月", "quarter" to "8 季", "year" to "5 年", "all" to "全部").forEach { (value, label) ->
            FilterChip(selected == value, { onSelected(value) }, label = { Text(label) })
        }
    }
}

@Composable
private fun DimensionRow(value: ActivityDimension) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(typeName(value.key), Modifier.weight(1f))
        Text("${value.views} 次 · ${formatSeconds(value.seconds)}")
    }
}

@Composable
private fun TrendChart(points: List<TrendPoint>) {
    var selected by rememberSaveable(points) { mutableIntStateOf((points.size - 1).coerceAtLeast(0)) }
    val color = MaterialTheme.colorScheme.primary
    if (points.isEmpty()) { Text("这个窗口没有趋势数据"); return }
    Canvas(Modifier.fillMaxWidth().height(164.dp).semantics { contentDescription = "浏览趋势，共 ${points.size} 个时段，可通过下方滑块选择" }
        .pointerInput(points) { detectTapGestures { selected = (it.x / size.width * (points.size - 1)).roundToInt().coerceIn(0, points.lastIndex) } }) {
        val maximum = points.maxOf { it.views }.coerceAtLeast(1).toFloat()
        val path = Path()
        points.forEachIndexed { index, value ->
            val x = index.toFloat() / (points.size - 1).coerceAtLeast(1) * size.width
            val y = size.height - 8.dp.toPx() - value.views / maximum * (size.height - 16.dp.toPx())
            if (index == 0) path.moveTo(x, y) else path.lineTo(x, y)
            drawCircle(color, if (index == selected) 5.dp.toPx() else 2.5.dp.toPx(), Offset(x, y))
        }
        val area = Path().apply { addPath(path); lineTo(size.width, size.height); lineTo(0f, size.height); close() }
        drawPath(area, color.copy(alpha = 0.10f))
        drawPath(path, color, style = Stroke(width = 2.dp.toPx()))
    }
    if (points.size > 1) Slider(selected.toFloat(), { selected = it.roundToInt() }, valueRange = 0f..points.lastIndex.toFloat(),
        steps = (points.size - 2).coerceAtLeast(0), modifier = Modifier.semantics { contentDescription = "选择趋势时段" })
    val point = points[selected.coerceIn(points.indices)]
    Text("${point.label} · 浏览 ${point.views} · 播放 ${point.plays} · 停留 ${formatSeconds(point.seconds)}", style = MaterialTheme.typography.bodySmall)
}

@Composable
fun ReviewScreen(model: LibraryViewModel, initialRange: String) {
    var range by rememberSaveable { mutableStateOf(initialRange) }
    val state by model.review.collectAsStateWithLifecycle()
    LaunchedEffect(range) { model.loadReview(range) }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).navigationBarsPadding().padding(vertical = 16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        RangeSelector(range) { range = it }
        if (state.loading) LinearProgressIndicator(Modifier.fillMaxWidth())
        state.error?.let { ErrorNotice(it) { model.loadReview(range) } }
        state.data?.let { report ->
            ContentCard(Modifier.padding(horizontal = 16.dp)) {
                Text("浏览时长", style = MaterialTheme.typography.titleMedium)
                Text(formatSeconds(report.totalSeconds), style = MaterialTheme.typography.displaySmall)
                HourChart(report.hours)
                Text("首次浏览 ${report.firstViews} · 再次浏览 ${report.returnViews}")
                Text("${report.distinctAssets} 件媒体 · 活跃 ${report.activeDays} 天")
            }
            if (!report.comparisonAvailable) ContentCard(Modifier.padding(horizontal = 16.dp)) { Text("没有可比较的上一时段") }
            listOf(true to "更多关注", false to "较少关注").forEach { (positive, label) ->
                val changes = report.changes.filter { if (positive) it.changePoints > 0 else it.changePoints < 0 }
                if (changes.isNotEmpty()) {
                    Text(label, Modifier.padding(horizontal = 20.dp), style = MaterialTheme.typography.titleMedium)
                    changes.forEach { InterestCard(it) }
                }
            }
        }
    }
}

@Composable
private fun HourChart(hours: List<ReviewHour>) {
    var selected by rememberSaveable { mutableIntStateOf(0) }
    val colors = listOf(Color(0xFF4D8DEE), Color(0xFFE8B24E), Color(0xFF52A87C))
    if (hours.isEmpty()) return
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) { listOf("0", "6", "12", "18", "24 时").forEach { Text(it, style = MaterialTheme.typography.labelSmall) } }
    Canvas(Modifier.fillMaxWidth().height(144.dp).semantics { contentDescription = "24 小时图片、动图和视频停留构成" }
        .pointerInput(hours) { detectTapGestures { selected = (it.x / size.width * hours.size).toInt().coerceIn(hours.indices) } }) {
        val max = hours.maxOf { it.seconds }.coerceAtLeast(1).toFloat()
        val cell = size.width / hours.size
        hours.forEachIndexed { index, hour ->
            var y = size.height
            listOf(hour.image, hour.animated, hour.video).forEachIndexed { part, value ->
                val height = value / max * size.height
                y -= height
                drawRect(colors[part], Offset(index * cell + 1.dp.toPx(), y), Size((cell - 2.dp.toPx()).coerceAtLeast(1f), height))
            }
        }
    }
    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) { listOf("图片", "动图", "视频").forEachIndexed { i, text -> Text("● $text", color = colors[i], style = MaterialTheme.typography.labelSmall) } }
    Slider(selected.toFloat(), { selected = it.roundToInt() }, valueRange = 0f..hours.lastIndex.toFloat(), steps = (hours.size - 2).coerceAtLeast(0),
        modifier = Modifier.semantics { contentDescription = "选择小时" })
    val hour = hours[selected.coerceIn(hours.indices)]
    Text("${hour.hour} 时 · ${formatSeconds(hour.seconds)} · ${hour.views} 次浏览", style = MaterialTheme.typography.bodySmall)
}

@Composable
private fun InterestCard(change: InterestChange) {
    var expanded by rememberSaveable(change.kind, change.id) { mutableStateOf(false) }
    ContentCard(Modifier.padding(horizontal = 16.dp)) {
        Text(change.name, style = MaterialTheme.typography.titleMedium)
        Text("${if (change.kind == "author") "作者" else "标签"} · 在浏览中的份额${if (change.changePoints > 0) "增加" else "减少"} %.1f 个百分点".format(abs(change.changePoints)))
        TextButton(onClick = { expanded = !expanded }, modifier = Modifier.semantics { stateDescription = if (expanded) "已展开" else "已收起" }) { Text(if (expanded) "收起" else "详情") }
        if (expanded) {
            Text("上期 %.1f%% / %d 次".format(change.previousShare, change.previousViews))
            Text("本期 %.1f%% / %d 次".format(change.currentShare, change.currentViews))
        }
    }
}

@Composable
fun RecommendationSettings(model: LibraryViewModel) {
    val state by model.recommendationPreferences.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == null) model.loadRecommendationPreferences() }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        ContentCard {
            Text("推荐偏好", style = MaterialTheme.typography.titleLarge)
            Column(Modifier.selectableGroup()) {
                state?.presets?.forEach { (key, weights) ->
                    val selected = state?.weights == weights
                    Row(Modifier.fillMaxWidth().selectable(selected, enabled = !busy, role = Role.RadioButton,
                        onClick = { if (!selected) model.chooseRecommendationPreset(weights) }).padding(vertical = 14.dp), verticalAlignment = Alignment.CenterVertically) {
                        RadioButton(selected, null, enabled = !busy)
                        Text(mapOf("balanced" to "均衡", "familiar" to "熟悉内容", "explore" to "探索", "fresh" to "新鲜内容")[key] ?: key, Modifier.padding(start = 12.dp))
                    }
                }
            }
            Box(Modifier.fillMaxWidth().heightIn(min = 48.dp)) {
                if (busy) LinearProgressIndicator(Modifier.fillMaxWidth())
                else if (message != null) ErrorNotice(message!!, model::loadRecommendationPreferences)
                else Text(if (state == null) "正在读取偏好…" else "已保存到当前服务器", style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

private fun formatSeconds(value: Long): String = if (value >= 3600) "${value / 3600} 小时 ${value % 3600 / 60} 分" else "${value / 60} 分 ${value % 60} 秒"
private fun typeName(value: String) = mapOf("image" to "图片", "animated_image" to "动图", "video" to "视频", "regular" to "常规", "cos" to "COS")[value] ?: value
