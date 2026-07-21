package com.localghost.app.ui

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.clickable
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import com.localghost.app.net.BoxClient
import com.localghost.app.ui.theme.*
import kotlin.math.PI
import kotlin.math.atan
import kotlin.math.exp
import kotlin.math.ln
import kotlin.math.tan

/**
 * The map, SELF-DRAWN , no tile servers, no map SDK, because every tile fetch ships the viewport
 * (and therefore where your photos are) to a third party, which is the one thing this product does
 * not do. Instead: Web Mercator on a Compose Canvas; landmass from the operator-provided Natural
 * Earth GeoJSON on the box (/v1/geo/world , public domain, a few MB, optional); your photo
 * locations from /v1/frames/geo as dots that thin out by zoom. No streets, no labels beyond the
 * tapped dot's place string , an outline world with YOUR data on it, fully offline, and it says so.
 */

private const val WORLD = 1024f // world size in map units at zoom 1

private fun invMercX(x: Float): Double = (x / WORLD) * 360.0 - 180.0

private fun invMercY(y: Float): Double {
    val n = PI - 2.0 * PI * (y / WORLD)
    return 180.0 / PI * kotlin.math.atan(kotlin.math.sinh(n))
}

private fun mercX(lon: Double): Float = ((lon + 180.0) / 360.0 * WORLD).toFloat()
private fun mercY(lat: Double): Float {
    val l = lat.coerceIn(-85.05, 85.05) * PI / 180.0
    return ((1.0 - ln(tan(l) + 1.0 / kotlin.math.cos(l)) / PI) / 2.0 * WORLD).toFloat()
}
private fun invLat(y: Float): Double {
    val n = PI - 2.0 * PI * y / WORLD
    return 180.0 / PI * atan(0.5 * (exp(n) - exp(-n)))
}
private fun invLon(x: Float): Double = x / WORLD * 360.0 - 180.0

/** One landmass ring, pre-projected to map units , parsed once, drawn every frame. */
private class Ring(val xs: FloatArray, val ys: FloatArray)

private fun parseWorld(gj: org.json.JSONObject): List<Ring> {
    val out = ArrayList<Ring>()
    val feats = gj.optJSONArray("features") ?: return out
    fun addRing(ring: org.json.JSONArray) {
        val n = ring.length()
        if (n < 3) return
        val xs = FloatArray(n); val ys = FloatArray(n)
        for (i in 0 until n) {
            val pt = ring.optJSONArray(i) ?: return
            xs[i] = mercX(pt.optDouble(0)); ys[i] = mercY(pt.optDouble(1))
        }
        out.add(Ring(xs, ys))
    }
    for (i in 0 until feats.length()) {
        val geom = feats.optJSONObject(i)?.optJSONObject("geometry") ?: continue
        val coords = geom.optJSONArray("coordinates") ?: continue
        when (geom.optString("type")) {
            "Polygon" -> for (r in 0 until coords.length()) coords.optJSONArray(r)?.let(::addRing)
            "MultiPolygon" -> for (p in 0 until coords.length()) {
                val poly = coords.optJSONArray(p) ?: continue
                for (r in 0 until poly.length()) poly.optJSONArray(r)?.let(::addRing)
            }
        }
    }
    return out
}

@Composable
fun MapScreen() {
    val ctx = LocalContext.current
    var rings by remember { mutableStateOf<List<Ring>>(emptyList()) }
    var points by remember { mutableStateOf<List<BoxClient.GeoPoint>>(emptyList()) }
    var picked by remember { mutableStateOf<BoxClient.GeoCell?>(null) }
    var viewer by remember { mutableStateOf<String?>(null) } // hash open full-screen
    var tracks by remember { mutableStateOf<List<List<Pair<Float, Float>>>>(emptyList()) }
    var loadNote by remember { mutableStateOf("loading…") }
    var cells by remember { mutableStateOf<List<BoxClient.GeoCell>>(emptyList()) }
    var level by remember { mutableStateOf(3) }
    var newest by remember { mutableStateOf<BoxClient.GeoCell?>(null) }
    LaunchedEffect(Unit) {
        // The world map (cached locally, ETag-revalidated) and the day tracks stay as they were;
        // the DOTS now come from the level-of-detail feed , postgres aggregates per zoom tier, so
        // a continent view ships a few hundred cells instead of every point in the archive.
        newest = BoxClient.newestGeoFrame(ctx)
        val world = BoxClient.worldGeoJson(ctx)
        rings = world?.let { runCatching { parseWorld(it) }.getOrDefault(emptyList()) } ?: emptyList()
        val days = BoxClient.geoDays(ctx, 14) ?: emptyList()
        val loaded = ArrayList<List<Pair<Float, Float>>>()
        for (d in days) {
            val pts = BoxClient.geoDayTrack(ctx, d) ?: continue
            if (pts.size >= 2) loaded.add(pts.map { Pair(mercX(it.second), mercY(it.first)) })
        }
        tracks = loaded
    }

    // Camera: centre in map units + zoom (pixels per map unit factor). Starts framing the DATA
    // when there is any, the whole world otherwise.
    var cx by remember { mutableStateOf(WORLD / 2f) }
    var cy by remember { mutableStateOf(WORLD / 2f) }
    var zoom by remember { mutableStateOf(1f) }
    // OPENS ON THE NEWEST PHOTO, close in , the map answers "where was I last" before it answers
    // "where have I ever been". Pan/pinch out from there; the whole archive is one gesture away.
    LaunchedEffect(newest, cells) {
        val nw = newest
        if (nw != null) {
            cx = mercX(nw.lon); cy = mercY(nw.lat)
            zoom = 6000f
        } else if (cells.isNotEmpty() && zoom <= 1f) {
            // Skew fallback: no newest endpoint , fit everything, like the map used to.
            val xs = cells.map { mercX(it.lon) }; val ys = cells.map { mercY(it.lat) }
            cx = (xs.min() + xs.max()) / 2f; cy = (ys.min() + ys.max()) / 2f
            val span = maxOf(xs.max() - xs.min(), ys.max() - ys.min(), 4f)
            zoom = (WORLD / span * 0.6f).coerceIn(1f, 400f)
        }
    }
    // THE LEVEL-OF-DETAIL LOOP. Zoom decides the tier, the viewport decides the bbox, and postgres
    // does the aggregating , panning a continent costs a few hundred rows, not the whole archive.
    // Debounced so a pinch does not fire twenty queries on its way to a resting zoom.
    var viewW by remember { mutableStateOf(1000f) }
    var viewH by remember { mutableStateOf(1000f) }
    LaunchedEffect(cx, cy, zoom, viewW, viewH) {
        kotlinx.coroutines.delay(220)
        val lvl = when {
            zoom < 20f -> 0
            zoom < 200f -> 1
            zoom < 3000f -> 2
            else -> 3
        }
        val pxz = (minOf(viewW, viewH) / WORLD) * zoom
        val halfW = (viewW / 2f) / pxz
        val halfH = (viewH / 2f) / pxz
        val latTop = invMercY(cy - halfH); val latBot = invMercY(cy + halfH)
        val lonL = invMercX(cx - halfW); val lonR = invMercX(cx + halfW)
        level = lvl
        val lod = BoxClient.framesGeoLod(ctx, lvl,
            minOf(latTop, latBot), maxOf(latTop, latBot),
            minOf(lonL, lonR), maxOf(lonL, lonR))
        cells = when {
            lod != null -> lod
            cells.isNotEmpty() -> cells
            else -> {
                // VERSION SKEW SHIELD , a new app against a box without the LOD endpoints must
                // still show a map. The old full-fetch endpoint carries every geotagged frame;
                // slower, but dots beat blankness until the operator redeploys.
                (BoxClient.framesGeo(ctx) ?: emptyList()).map {
                    BoxClient.GeoCell(it.lat, it.lon, 1, it.hash, it.takenAt)
                }
            }
        }
        loadNote = when {
            cells.isEmpty() -> "no geotagged photos in view"
            else -> cells.sumOf { it.n }.toString() + " photos · detail " + (lvl + 1) + "/4"
        }
    }
    Column(Modifier.fillMaxSize()) {
        Text("> MAP", color = TerminalGreen, style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.padding(16.dp))
        Text(loadNote, color = GhostTextDim, style = MaterialTheme.typography.labelMedium,
            modifier = Modifier.padding(horizontal = 16.dp))
        Box(Modifier.weight(1f).fillMaxWidth().padding(12.dp).background(Void)) {
            Canvas(Modifier.fillMaxSize()
                .pointerInput(Unit) {
                    detectTransformGestures { centroid, pan, gz, _ ->
                        // zoom about the finger centroid, then pan , standard camera algebra
                        val newZoom = (zoom * gz).coerceIn(0.8f, 250000f)
                        val sw = size.width.toFloat(); val sh = size.height.toFloat()
                        val scale = (minOf(sw, sh) / WORLD)
                        val px = { z: Float -> scale * z }
                        val wx = cx + (centroid.x - sw / 2f) / px(zoom)
                        val wy = cy + (centroid.y - sh / 2f) / px(zoom)
                        cx = wx - (centroid.x - sw / 2f) / px(newZoom)
                        cy = wy - (centroid.y - sh / 2f) / px(newZoom)
                        zoom = newZoom
                        cx -= pan.x / px(zoom); cy -= pan.y / px(zoom)
                        picked = null
                    }
                }
                .pointerInput(cells) {
                    detectTapGestures { tap ->
                        val sw = size.width.toFloat(); val sh = size.height.toFloat()
                        val pxz = (minOf(sw, sh) / WORLD) * zoom
                        var best: BoxClient.GeoCell? = null; var bestD = 44f * 44f
                        cells.forEach { p ->
                            val px = (mercX(p.lon) - cx) * pxz + sw / 2f
                            val py = (mercY(p.lat) - cy) * pxz + sh / 2f
                            val d = (px - tap.x) * (px - tap.x) + (py - tap.y) * (py - tap.y)
                            if (d < bestD) { bestD = d; best = p }
                        }
                        picked = best
                        // A CLUSTER tap dives in (centre it and zoom a tier); a single photo opens.
                        best?.let { b ->
                            if (b.n > 1) {
                                cx = mercX(b.lon); cy = mercY(b.lat)
                                zoom = (zoom * 6f).coerceAtMost(250000f)
                            }
                        }
                    }
                }) {
                val sw = size.width; val sh = size.height
                viewW = sw; viewH = sh
                val pxz = (minOf(sw, sh) / WORLD) * zoom
                fun sx(x: Float) = (x - cx) * pxz + sw / 2f
                fun sy(y: Float) = (y - cy) * pxz + sh / 2f
                // graticule every 15 degrees , always drawn, the honest skeleton of the projection
                var lon = -180.0
                while (lon <= 180.0) {
                    val x = sx(mercX(lon))
                    if (x in -2f..sw + 2f) drawLine(GhostBorder, Offset(x, 0f), Offset(x, sh), 1f)
                    lon += 15.0
                }
                var lat = -75.0
                while (lat <= 75.0) {
                    val y = sy(mercY(lat))
                    if (y in -2f..sh + 2f) drawLine(GhostBorder, Offset(0f, y), Offset(sw, y), 1f)
                    lat += 15.0
                }
                // day tracks , movement under the moments, dimmer than the dots they connect
                tracks.forEach { tr ->
                    val path = Path()
                    path.moveTo(sx(tr[0].first), sy(tr[0].second))
                    for (i in 1 until tr.size) path.lineTo(sx(tr[i].first), sy(tr[i].second))
                    drawPath(path, TerminalDim, style = Stroke(width = 2.5f))
                }
                // landmass outlines
                rings.forEach { r ->
                    val path = Path()
                    path.moveTo(sx(r.xs[0]), sy(r.ys[0]))
                    for (i in 1 until r.xs.size) path.lineTo(sx(r.xs[i]), sy(r.ys[i]))
                    path.close()
                    drawPath(path, TerminalDim, style = Stroke(width = 1.5f))
                }
                // photo dots , the point of the whole screen. At LOW zoom, thousands of dots
                // smear into blobs; grid-bucket them into CLUSTERS with counts instead (48px
                // cells), and let individual dots return once zoom spreads them out. The
                // threshold is dot density, not zoom level , sparse archives never cluster.
                // Dots come pre-aggregated from the box: n == 1 is a photo, n > 1 is a cell with
                // a count. No client-side bucketing , the tier already did it, which is why a
                // continent view is a few hundred draws instead of fifty thousand.
                cells.forEach { p ->
                    val x = sx(mercX(p.lon)); val y = sy(mercY(p.lat))
                    if (x in -24f..sw + 24f && y in -24f..sh + 24f) {
                        if (p.n <= 1) {
                            drawCircle(TerminalGreen, radius = 5f, center = Offset(x, y))
                        } else {
                            val r = (9f + 3.2f * kotlin.math.ln(p.n.toFloat())).coerceAtMost(26f)
                            drawCircle(TerminalGreen.copy(alpha = 0.22f), radius = r, center = Offset(x, y))
                            drawCircle(TerminalGreen, radius = r, center = Offset(x, y), style = Stroke(width = 1.5f))
                            drawContext.canvas.nativeCanvas.drawText(
                                if (p.n > 999) "999+" else p.n.toString(), x, y + 4f,
                                android.graphics.Paint().apply {
                                    color = android.graphics.Color.rgb(0x39, 0xFF, 0x14)
                                    textSize = 11f * density
                                    textAlign = android.graphics.Paint.Align.CENTER
                                    typeface = android.graphics.Typeface.MONOSPACE
                                })
                        }
                    }
                }
                picked?.let { p ->
                    val x = sx(mercX(p.lon)); val y = sy(mercY(p.lat))
                    drawCircle(TerminalGreen, radius = 10f, center = Offset(x, y), style = Stroke(2f))
                }
            }
        }
        viewer?.let { h -> ImageViewer(h, onDismiss = { viewer = null }) }
        picked?.let { p ->
            Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                Text(
                    (if (p.n > 1) "${p.n} photos here" else "%.5f, %.5f".format(p.lat, p.lon)) +
                        (if (p.takenAt > 0) "  ·  " + java.text.SimpleDateFormat("MMM d, yyyy", java.util.Locale.US)
                            .format(java.util.Date(p.takenAt * 1000)) else ""),
                    color = GhostText, style = MaterialTheme.typography.labelMedium)
                if (p.n == 1 && p.hash.isNotEmpty()) {
                    Text("  [ open ]", color = TerminalGreen,
                        style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.clickable { viewer = p.hash })
                }
            }
        }
    }
}