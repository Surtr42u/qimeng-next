package media.qimeng.next.screen

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
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
fun ActivityRankingScreen(model: LibraryViewModel, kind: String, range: String) {
    var metric by rememberSaveable(kind, range) { mutableStateOf("views") }
    val state by model.ranking.collectAsStateWithLifecycle()
    LaunchedEffect(kind, range, metric) { model.loadRanking(kind, range, metric) }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { Text(rankingTitle(kind), style = MaterialTheme.typography.titleLarge) }
        if (kind == "assets") item { Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(metric == "views", { metric = "views" }, label = { Text("浏览次数") })
            FilterChip(metric == "seconds", { metric = "seconds" }, label = { Text("停留时长") })
        } }
        if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
        state.error?.let { item { ErrorNotice(it) { model.loadRanking(kind, range, metric) } } }
        if (state.loaded && state.items.isEmpty()) item { Text("这个窗口暂无记录") }
        items(state.items, key = { it.id }) { entry -> StatsCard(onClick = { openRank(model, kind, entry) }) {
            Text(entry.name, style = MaterialTheme.typography.titleMedium)
            Text(if (metric == "seconds") formatSeconds(entry.value) else "${entry.value} 次浏览")
            Text("${entry.assets} 件媒体 · 播放 ${entry.plays} 次 · 停留 ${formatSeconds(entry.seconds)}", style = MaterialTheme.typography.bodySmall)
        } }
        if (state.cursor != null) item { TextButton(onClick = { model.loadRanking(kind, range, metric, true) }, enabled = !state.loading) { Text("加载更多") } }
    }
}

private fun rankingTitle(kind: String) = when (kind) { "authors" -> "常看作者"; "tags" -> "常看标签"; else -> "最常浏览的媒体" }
private fun openRank(model: LibraryViewModel, kind: String, entry: ActivityRank) = when (kind) {
    "authors" -> model.openAuthor(entry.id)
    "tags" -> model.navigate("tag:${entry.name}")
    else -> entry.assetId?.let { model.openAsset(it) }
}

@Composable
fun ReviewScreen(model: LibraryViewModel, initialRange: String) {
    var range by rememberSaveable { mutableStateOf(initialRange) }
    val state by model.review.collectAsStateWithLifecycle()
    LaunchedEffect(range) { model.loadReview(range) }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).navigationBarsPadding().padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        RangeSelector(range) { range = it }
        if (state.loading) LinearProgressIndicator(Modifier.fillMaxWidth())
        state.error?.let { ErrorNotice(it) { model.loadReview(range) } }
        state.data?.takeIf { it.range == range }?.let { report ->
            StatsCard {
                Text("浏览时长", style = MaterialTheme.typography.titleMedium)
                Text(formatSeconds(report.totalSeconds), style = MaterialTheme.typography.displaySmall)
                HourChart(report.hours)
                Text("首次浏览 ${report.firstViews} · 再次浏览 ${report.returnViews}")
                Text("${report.distinctAssets} 件媒体 · 活跃 ${report.activeDays} 天")
            }
            if (!report.comparisonAvailable) StatsCard { Text("没有可比较的上一时段") }
            listOf(true to "更多关注", false to "较少关注").forEach { (positive, label) ->
                val changes = report.changes.filter { if (positive) it.changePoints > 0 else it.changePoints < 0 }
                if (changes.isNotEmpty()) {
                    Text(label, Modifier.padding(horizontal = 4.dp), style = MaterialTheme.typography.titleMedium)
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
    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        listOf("图片", "动图", "视频").forEachIndexed { index, label ->
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                Box(Modifier.size(8.dp).background(colors[index]))
                Text(label, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelSmall)
            }
        }
    }
    Slider(selected.toFloat(), { selected = it.roundToInt() }, valueRange = 0f..hours.lastIndex.toFloat(), steps = (hours.size - 2).coerceAtLeast(0),
        modifier = Modifier.semantics { contentDescription = "选择小时" })
    val hour = hours[selected.coerceIn(hours.indices)]
    Text("${hour.hour} 时 · ${formatSeconds(hour.seconds)} · ${hour.views} 次浏览", style = MaterialTheme.typography.bodySmall)
}

@Composable
private fun InterestCard(change: InterestChange) {
    var expanded by rememberSaveable(change.kind, change.id) { mutableStateOf(false) }
    StatsCard {
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
internal fun typeName(value: String) = mapOf("image" to "图片", "animated_image" to "动图", "video" to "视频", "regular" to "常规", "cos" to "COS")[value] ?: value
