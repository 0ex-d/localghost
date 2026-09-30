package com.localghost.app.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class QrApertureModelTest {
    @Test fun aSingleCleanCodeFillsTheRingAtOnce() {
        assertEquals(0, QrApertureModel.litSegments(0, 1))
        assertEquals(QrApertureModel.SEGMENTS, QrApertureModel.litSegments(1, 1))
        assertEquals(1f, QrApertureModel.progress(1, 1))
    }

    @Test fun aMultiFrameCodeFillsAsFramesLand() {
        // eight of twelve is the erasure-coded threshold; the ring is proportional
        assertEquals(0, QrApertureModel.litSegments(0, 8))
        assertEquals(1, QrApertureModel.litSegments(1, 8))          // always at least one while reading
        assertEquals(6, QrApertureModel.litSegments(4, 8))          // half of twelve
        assertEquals(QrApertureModel.SEGMENTS, QrApertureModel.litSegments(8, 8))
        assertTrue(QrApertureModel.progress(4, 8) in 0.49f..0.51f)
    }

    @Test fun segmentAnglesRingTheClock() {
        assertEquals(-90f, QrApertureModel.segmentAngle(0), 0.001f)       // first at the top
        assertEquals(-90f + 30f, QrApertureModel.segmentAngle(1), 0.001f) // 12 segments, 30 apart
    }

    @Test fun irisEasesOpen() {
        assertEquals(0f, QrApertureModel.irisOpen(0, 550), 0.001f)
        assertEquals(1f, QrApertureModel.irisOpen(550, 550), 0.001f)
        assertTrue(QrApertureModel.irisOpen(275, 550) > 0.5f) // ease-out is past halfway at the midpoint
    }

    @Test fun phaseFollowsWhatTheScannerSees() {
        assertEquals(QrApertureModel.Phase.HUNTING, QrApertureModel.phase(false, 0, wrong = false, found = false))
        assertEquals(QrApertureModel.Phase.LOCKING, QrApertureModel.phase(true, 0, wrong = false, found = false))
        assertEquals(QrApertureModel.Phase.READING, QrApertureModel.phase(true, 3, wrong = false, found = false))
        assertEquals(QrApertureModel.Phase.WRONG, QrApertureModel.phase(true, 0, wrong = true, found = false))
        assertEquals(QrApertureModel.Phase.FOUND, QrApertureModel.phase(true, 8, wrong = false, found = true))
    }
}
