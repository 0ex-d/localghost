package com.localghost.app.local

import com.localghost.app.ui.LandTileGeom
import com.localghost.app.ui.RoadTileGeom
import kotlin.math.cos
import kotlin.math.floor
import kotlin.math.sqrt

/**
 * WHICH MAP TILES TO KEEP ON THE PHONE, in the order they are worth having. The map fetches tiles
 * from the box as you look (fast at home, slow the first time anywhere else); with "download maps"
 * on, the phone fills its caches ahead of time on Wi-Fi, within a size the person picked.
 *
 * The order: first every street tile within [fineRadiusKm] of the places this phone has been
 * lately (where you zoom in most), nearest first; then the coast and the major roads, nearest
 * first from the main place outwards, so a smaller budget still covers home and its region and a
 * larger one reaches further. Pure: the worker walks the list, the tests check it.
 */
internal object MapPlan {
    /** One tile to have: kind "land" (a coast tile) or "road" (level 1 major, 0 streets). */
    data class Tile(val kind: String, val level: Int, val x: Int, val y: Int, val km: Double)

    /** A place the phone has been, with how much it counts (fixes seen there). */
    data class Center(val lat: Double, val lon: Double, val weight: Int = 1)

    /**
     * The places from a phone's recent fixes: the busiest tenth-of-a-degree cells (about 11 km),
     * busiest first, at most [max]. The first is where the phone spends its time.
     */
    fun centers(fixes: List<Pair<Double, Double>>, max: Int = 3): List<Center> {
        if (fixes.isEmpty()) return emptyList()
        val cells = HashMap<Long, IntArray>() // key -> [count]
        val sums = HashMap<Long, DoubleArray>() // key -> [latSum, lonSum]
        for ((lat, lon) in fixes) {
            val k = floor((lat + 90) * 10).toLong() * 4000 + floor((lon + 180) * 10).toLong()
            cells.getOrPut(k) { IntArray(1) }[0]++
            val s = sums.getOrPut(k) { DoubleArray(2) }
            s[0] += lat; s[1] += lon
        }
        return cells.entries.sortedByDescending { it.value[0] }.take(max).map { (k, c) ->
            val s = sums.getValue(k)
            Center(s[0] / c[0], s[1] / c[0], c[0])
        }
    }

    /** Kilometres between two points, near enough for ordering tiles (equirectangular). */
    fun km(lat1: Double, lon1: Double, lat2: Double, lon2: Double): Double {
        val x = Math.toRadians(lon2 - lon1) * cos(Math.toRadians((lat1 + lat2) / 2))
        val y = Math.toRadians(lat2 - lat1)
        return sqrt(x * x + y * y) * 6371.0
    }

    /**
     * The tiles in the order to fetch them. [land] is the coast index (a state per one-degree
     * cell; COAST cells have tiles), [roads] the road index; either may be null (the box has not
     * cut them). No centers: nothing (without a place, "nearest" means nothing).
     */
    fun plan(centers: List<Center>, land: ByteArray?, roads: RoadTileGeom.Index?, fineRadiusKm: Double = 25.0): List<Tile> {
        if (centers.isEmpty()) return emptyList()
        val out = ArrayList<Tile>()
        // 1. streets around each place, the main place first
        if (roads != null) {
            val seen = HashSet<Int>()
            for (c in centers) {
                val dLat = fineRadiusKm / 111.0
                val dLon = fineRadiusKm / (111.0 * cos(Math.toRadians(c.lat)).coerceAtLeast(0.05))
                val x0 = floor((c.lon - dLon + 180) * 10).toInt().coerceIn(0, RoadTileGeom.FINE_COLS - 1)
                val x1 = floor((c.lon + dLon + 180) * 10).toInt().coerceIn(0, RoadTileGeom.FINE_COLS - 1)
                val y0 = floor((c.lat - dLat + 90) * 10).toInt().coerceIn(0, RoadTileGeom.FINE_ROWS - 1)
                val y1 = floor((c.lat + dLat + 90) * 10).toInt().coerceIn(0, RoadTileGeom.FINE_ROWS - 1)
                val here = ArrayList<Tile>()
                for (y in y0..y1) for (x in x0..x1) {
                    if (!roads.hasFine(x, y) || !seen.add(y * RoadTileGeom.FINE_COLS + x)) continue
                    val d = km(c.lat, c.lon, y / 10.0 - 90 + 0.05, x / 10.0 - 180 + 0.05)
                    if (d <= fineRadiusKm + 8) here.add(Tile("road", 0, x, y, d)) // +8: a cell's half-diagonal
                }
                out.addAll(here.sortedBy { it.km })
            }
        }
        // 2. the coast and the major roads, nearest first from the main place
        val home = centers.first()
        val wide = ArrayList<Tile>()
        if (land != null && land.size == LandTileGeom.COLS * LandTileGeom.ROWS) {
            for (y in 0 until LandTileGeom.ROWS) for (x in 0 until LandTileGeom.COLS) {
                if (land[y * LandTileGeom.COLS + x].toInt() != LandTileGeom.COAST) continue
                wide.add(Tile("land", 1, x, y, km(home.lat, home.lon, y - 90 + 0.5, x - 180 + 0.5)))
            }
        }
        if (roads != null) {
            for (y in 0 until RoadTileGeom.MAJOR_ROWS) for (x in 0 until RoadTileGeom.MAJOR_COLS) {
                if (!roads.hasMajor(x, y)) continue
                wide.add(Tile("road", 1, x, y, km(home.lat, home.lon, y - 90 + 0.5, x - 180 + 0.5)))
            }
        }
        out.addAll(wide.sortedBy { it.km })
        return out
    }
}
