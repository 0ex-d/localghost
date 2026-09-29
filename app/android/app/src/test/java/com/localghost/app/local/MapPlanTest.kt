package com.localghost.app.local

import com.localghost.app.ui.LandTileGeom
import com.localghost.app.ui.RoadTileGeom
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class MapPlanTest {
    private val london = MapPlan.Center(51.5, -0.12)

    private fun roads(majors: List<Pair<Int, Int>>, fines: List<Pair<Int, Int>>): RoadTileGeom.Index {
        val major = ByteArray(RoadTileGeom.MAJOR_COLS * RoadTileGeom.MAJOR_ROWS)
        majors.forEach { (x, y) -> major[y * RoadTileGeom.MAJOR_COLS + x] = 1 }
        val fine = ByteArray(RoadTileGeom.FINE_COLS * RoadTileGeom.FINE_ROWS / 8)
        fines.forEach { (x, y) -> val k = y * RoadTileGeom.FINE_COLS + x; fine[k shr 3] = (fine[k shr 3].toInt() or (1 shl (k and 7))).toByte() }
        return RoadTileGeom.Index(major, fine)
    }
    private fun fineCell(lat: Double, lon: Double) = ((lon + 180) * 10).toInt() to ((lat + 90) * 10).toInt()
    private fun cell(lat: Double, lon: Double) = (Math.floor(lon).toInt() + 180) to (Math.floor(lat).toInt() + 90)

    @Test fun streetsAroundHomeFirstThenTheRegionNearestFirst() {
        val land = ByteArray(LandTileGeom.COLS * LandTileGeom.ROWS)
        val (dx, dy) = cell(51.1, 1.3) // Dover's coast, near
        land[dy * LandTileGeom.COLS + dx] = LandTileGeom.COAST.toByte()
        val (nx, ny) = cell(40.6, -74.0) // New York's coast, far
        land[ny * LandTileGeom.COLS + nx] = LandTileGeom.COAST.toByte()
        val (lx, ly) = cell(51.5, -0.12)
        land[ly * LandTileGeom.COLS + lx] = LandTileGeom.LAND.toByte() // inland: no tile
        val r = roads(
            majors = listOf(cell(51.5, -0.12), cell(48.85, 2.35)), // London, Paris
            fines = listOf(fineCell(51.5, -0.12), fineCell(51.6, -0.2), fineCell(52.2, 0.12)), // two near, Cambridge beyond 25 km
        )
        val p = MapPlan.plan(listOf(london), land, r)
        assertEquals(listOf("road0", "road0", "road1", "land1", "road1", "land1"), p.map { it.kind + it.level })
        assertTrue("home street tile first", p[0].km < p[1].km)
        assertTrue("then London's major roads (0 km), Dover's coast, Paris, New York", p[2].km < p[3].km && p[3].km < p[4].km && p[4].km < p[5].km)
        assertTrue("Cambridge's streets are beyond the radius", p.none { it.level == 0 && it.km > 33 })
    }

    @Test fun centersAreTheBusiestPlaces() {
        val fixes = List(10) { 51.50 to -0.12 } + List(3) { 48.85 to 2.35 } + listOf(40.7 to -74.0)
        val c = MapPlan.centers(fixes, max = 2)
        assertEquals(2, c.size)
        assertEquals(51.5, c[0].lat, 0.01)
        assertEquals(10, c[0].weight)
        assertEquals(48.85, c[1].lat, 0.01)
        assertTrue(MapPlan.centers(emptyList()).isEmpty())
        assertTrue("no place, no plan", MapPlan.plan(emptyList(), null, null).isEmpty())
    }

    @Test fun distance() {
        assertEquals(344.0, MapPlan.km(51.5, -0.12, 48.85, 2.35), 15.0) // London to Paris
    }
}
