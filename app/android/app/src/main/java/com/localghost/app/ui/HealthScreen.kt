package com.localghost.app.ui

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import com.localghost.app.net.BoxClient
import com.localghost.app.ui.theme.*

/**
 * HEALTH , tallyd's drill-in screen, the first of the per-daemon screens. Everything here is the
 * box's own copy of the phone's Health Connect data: 30-day daily series per metric, drawn as bar
 * strips with min/avg/max , stats, not judgements. The pattern for the rest of the fleet: one
 * screen per daemon, the daemon's domain drawn from its own tables.
 */

private val metricLabels = mapOf(
    "steps" to "steps", "sleep_minutes" to "sleep", "exercise_minutes" to "exercise",
    "distance_km" to "distance", "calories" to "calories", "floors" to "floors",
    "hr_avg" to "heart rate (avg)", "hr_min" to "heart rate (min)", "hr_max" to "heart rate (max)",
    "weight_kg" to "weight",
)

private fun fmtVal(metric: String, v: Double): String = when (metric) {
    "sleep_minutes" -> "${(v / 60).toInt()}h${"%02d".format((v % 60).toInt())}m"
    "distance_km" -> "%.1f km".format(v)
    "weight_kg" -> "%.1f kg".format(v)
    "exercise_minutes" -> "${v.toInt()}m"
    else -> "%,d".format(v.toInt())
}

@Composable
fun HealthScreen() {
    val ctx = LocalContext.current
    var series by remember { mutableStateOf<List<BoxClient.HealthSeries>?>(null) }
    LaunchedEffect(Unit) { series = BoxClient.healthStats(ctx, 30) }
    LazyColumn(Modifier.fillMaxSize().padding(horizontal = 20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item {
            Spacer(Modifier.height(12.dp))
            SectionLabel("HEALTH")
            Spacer(Modifier.height(4.dp))
            Text("30 days · your box's copy · synced from Health Connect on the SYNC screen",
                color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
        }
        when {
            series == null -> item {
                Text("reading from the box…", color = GhostTextDim,
                    style = MaterialTheme.typography.bodyMedium)
            }
            series!!.isEmpty() -> item {
                Text("! no health data yet , CONNECT HEALTH on the SYNC screen and sync a week",
                    color = TerminalDim, style = MaterialTheme.typography.bodyMedium)
            }
            else -> {
                val order = listOf("steps", "sleep_minutes", "exercise_minutes", "distance_km",
                    "calories", "floors", "hr_avg", "hr_max", "hr_min", "weight_kg")
                val sorted = series!!.filter { it.values.isNotEmpty() }
                    .sortedBy { order.indexOf(it.metric).let { i -> if (i < 0) 99 else i } }
                items(sorted, key = { it.metric }) { s ->
                MetricCard(s)
                }
            }
        }
        item { Spacer(Modifier.height(20.dp)) }
    }
}

@Composable
private fun MetricCard(s: BoxClient.HealthSeries) {
    val label = metricLabels[s.metric] ?: s.metric
    val latest = s.values.last()
    val mn = s.values.min(); val mx = s.values.max(); val avg = s.values.average()
    Column(Modifier.fillMaxWidth().animateContentSize().border(1.dp, GhostBorder, RectangleShape).background(Void).padding(12.dp)) {
        Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
            Text("> $label", color = TerminalGreen, style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.weight(1f))
            Text(fmtVal(s.metric, latest), color = GhostText, style = MaterialTheme.typography.bodyMedium)
        }
        Spacer(Modifier.height(6.dp))
        // 30 bars, scaled to the metric's own range , shape over scale, honest zero baseline.
        Canvas(Modifier.fillMaxWidth().height(44.dp)) {
            val n = s.values.size
            if (n == 0) return@Canvas
            val bw = size.width / n
            val top = if (mx > 0) mx else 1.0
            s.values.forEachIndexed { i, v ->
                val h = (v / top).toFloat() * size.height
                drawRect(TerminalGreen, topLeft = Offset(i * bw + bw * 0.15f, size.height - h),
                    size = Size(bw * 0.7f, h))
            }
        }
        Spacer(Modifier.height(4.dp))
        Text("min ${fmtVal(s.metric, mn)} · avg ${fmtVal(s.metric, avg)} · max ${fmtVal(s.metric, mx)}",
            color = TerminalDim, style = MaterialTheme.typography.labelMedium)
    }
}
