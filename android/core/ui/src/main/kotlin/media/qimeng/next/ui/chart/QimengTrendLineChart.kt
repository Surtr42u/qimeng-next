package media.qimeng.next.ui.chart

import androidx.compose.animation.core.tween
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.dp
import com.patrykandpatrick.vico.compose.cartesian.CartesianChartHost
import com.patrykandpatrick.vico.compose.cartesian.axis.rememberBottom
import com.patrykandpatrick.vico.compose.cartesian.axis.rememberStart
import com.patrykandpatrick.vico.compose.cartesian.layer.point
import com.patrykandpatrick.vico.compose.cartesian.layer.rememberLine
import com.patrykandpatrick.vico.compose.cartesian.layer.rememberLineCartesianLayer
import com.patrykandpatrick.vico.compose.cartesian.rememberCartesianChart
import com.patrykandpatrick.vico.compose.common.component.rememberTextComponent
import com.patrykandpatrick.vico.compose.common.component.shapeComponent
import com.patrykandpatrick.vico.compose.common.fill
import com.patrykandpatrick.vico.compose.common.shape.rounded
import com.patrykandpatrick.vico.core.cartesian.CartesianChart
import com.patrykandpatrick.vico.core.cartesian.axis.HorizontalAxis
import com.patrykandpatrick.vico.core.cartesian.axis.VerticalAxis
import com.patrykandpatrick.vico.core.cartesian.data.CartesianChartModel
import com.patrykandpatrick.vico.core.cartesian.data.CartesianChartModelProducer
import com.patrykandpatrick.vico.core.cartesian.data.CartesianValueFormatter
import com.patrykandpatrick.vico.core.cartesian.data.LineCartesianLayerModel
import com.patrykandpatrick.vico.core.cartesian.layer.LineCartesianLayer
import com.patrykandpatrick.vico.core.common.data.ExtraStore
import com.patrykandpatrick.vico.core.common.shape.CorneredShape
import media.qimeng.next.ui.LocalReducedMotion
import kotlin.math.abs
import kotlin.math.roundToInt

data class QimengTrendSeries(val values: List<Number>, val name: String = "")

/** Retained old line/area/point/axis presentation; Vico owns chart coordinates and rendering. */
@Composable
fun QimengTrendLineChart(
    series: List<QimengTrendSeries>,
    seriesColors: List<Color>,
    xLabels: List<String>,
    modifier: Modifier = Modifier,
    chartTitle: String = "浏览趋势",
) {
    require(seriesColors.size >= series.size)
    if (series.isEmpty() || xLabels.isEmpty()) return
    var selected by rememberSaveable(xLabels) { mutableStateOf<Double?>(null) }
    val lines = remember(series) {
        if (series.any { it.values.isEmpty() }) null
        else LineCartesianLayerModel.partial { series.forEach { line -> series(line.values) } }
    }
    val pointInnerColor = MaterialTheme.colorScheme.surfaceVariant
    val marker = rememberTrendValueMarker(seriesColors.zip(series.map { it.name }).toMap())
    val layer = rememberLineCartesianLayer(lineProvider = LineCartesianLayer.LineProvider.series(
        seriesColors.take(series.size).map { color ->
            LineCartesianLayer.rememberLine(
                fill = LineCartesianLayer.LineFill.single(fill(color)),
                areaFill = LineCartesianLayer.AreaFill.single(fill(color.copy(alpha = 40f / 255f))),
                pointProvider = LineCartesianLayer.PointProvider.single(LineCartesianLayer.point(
                    component = shapeComponent(fill = fill(pointInnerColor), shape = CorneredShape.rounded(50),
                        strokeFill = fill(color), strokeThickness = 2.dp), size = 8.dp)))
        }))
    val selectedX = selected
    val persistent: (CartesianChart.PersistentMarkerScope.(ExtraStore) -> Unit)? = remember(marker, selectedX) {
        if (selectedX == null) null else { _ -> marker at selectedX }
    }
    val chart = rememberCartesianChart(layer,
        startAxis = VerticalAxis.rememberStart(line = null, tick = null, label = null, guideline = null),
        bottomAxis = HorizontalAxis.rememberBottom(line = null, tick = null, guideline = null,
            label = rememberTextComponent(color = MaterialTheme.colorScheme.onSurfaceVariant),
            valueFormatter = CartesianValueFormatter { _, value, _ -> xLabels.getOrNull(value.roundToInt()).orEmpty() }),
        persistentMarkers = persistent)
    val latestLayer by rememberUpdatedState(layer)
    val description = selected?.roundToInt()?.let { index ->
        xLabels.getOrNull(index)?.let { date -> "$date · " + series.joinToString(" · ") { line -> "${line.name} ${line.values.getOrNull(index) ?: "—"}次" } }
    } ?: "未选择数据点"
    val reduceMotion = LocalReducedMotion.current
    val chartModifier = modifier
        .pointerInput(xLabels) {
            detectTapGestures { tap ->
                // Public library targets already include layout, scrolling and zoom; no coordinate guess.
                val target = latestLayer.markerTargets.values.flatten().minByOrNull { abs(it.canvasX - tap.x) }
                target?.let { selected = if (selected == it.x) null else it.x }
            }
        }.semantics {
            contentDescription = "$chartTitle，共 ${xLabels.size} 个时段"
            stateDescription = description
            customActions = listOf(
                CustomAccessibilityAction("下一个数据点") {
                    if (xLabels.isEmpty()) false else { selected = (((selected?.roundToInt() ?: -1) + 1) % xLabels.size).toDouble(); true }
                },
                CustomAccessibilityAction("隐藏数值") { selected = null; true })
        }
    if (reduceMotion && lines != null) {
        val model = remember(lines) { CartesianChartModel(lines.complete(ExtraStore.Empty)) }
        CartesianChartHost(chart, model, modifier = chartModifier)
    } else {
        val producer = remember { CartesianChartModelProducer() }
        LaunchedEffect(producer, lines) { producer.runTransaction { lines?.let { add(it) } } }
        CartesianChartHost(chart, producer, modifier = chartModifier,
            animationSpec = if (reduceMotion) null else tween(500), animateIn = !reduceMotion)
    }
}
