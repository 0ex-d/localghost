package com.localghost.app.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class QrApertureModelTest {
    @Test fun aSingleCleanCodeFillsTheRingAtOnce() {
        assertEquals(0, QrApertureModel.litSegments(0, 1))
        assertEquals(QrApertureModel.SEGMENTS, QrApertureModel.litSegments(1, 1))
        assertEquals(1f, QrApertureModel.progress(1, 1))
    }

    @Test fun eightSegmentsOnePerFrame() {
        assertEquals(8, QrApertureModel.SEGMENTS)
        assertEquals(0, QrApertureModel.litSegments(0, 8))
        assertEquals(1, QrApertureModel.litSegments(1, 8))          // always at least one while reading
        assertEquals(4, QrApertureModel.litSegments(4, 8))          // one segment per frame
        assertEquals(8, QrApertureModel.litSegments(8, 8))
        assertTrue(QrApertureModel.progress(4, 8) in 0.49f..0.51f)
    }

    @Test fun segmentAnglesRingTheClock() {
        assertEquals(-90f, QrApertureModel.segmentAngle(0), 0.001f)       // first at the top
        assertEquals(-90f + 45f, QrApertureModel.segmentAngle(1), 0.001f) // 8 segments, 45 apart
    }

    @Test fun establishWalksTheStepsThenHoldsReady() {
        // at the start, the first step, nothing done yet
        assertEquals(QrApertureModel.Step.IDENTITY, QrApertureModel.stepAt(0f))
        assertEquals(0, QrApertureModel.stepsDone(0f))
        assertFalse(QrApertureModel.ready(0f))
        // a quarter of the way through the steps portion is the second step
        val q = QrApertureModel.STEPS_FRAC * 0.30f
        assertEquals(QrApertureModel.Step.CHANNEL, QrApertureModel.stepAt(q))
        assertEquals(1, QrApertureModel.stepsDone(q))
        // just before the steps end, the last step
        assertEquals(QrApertureModel.Step.PINNED, QrApertureModel.stepAt(QrApertureModel.STEPS_FRAC - 0.01f))
        // past the steps portion it is READY, all four done, no current step
        assertTrue(QrApertureModel.ready(0.9f))
        assertEquals(null, QrApertureModel.stepAt(0.9f))
        assertEquals(4, QrApertureModel.stepsDone(0.9f))
        assertEquals(1f, QrApertureModel.stepProgress(0.9f), 0.001f)
    }

    @Test fun stepProgressRunsZeroToOneWithinAStep() {
        val each = QrApertureModel.STEPS_FRAC / QrApertureModel.Step.entries.size
        assertTrue(QrApertureModel.stepProgress(0.001f) < 0.1f)          // near the start of step 0
        assertTrue(QrApertureModel.stepProgress(each * 0.99f) > 0.9f)    // near the end of step 0
    }

    @Test fun phaseFollowsWhatTheScannerSees() {
        assertEquals(QrApertureModel.Phase.HUNTING, QrApertureModel.phase(false, 0, wrong = false, found = false))
        assertEquals(QrApertureModel.Phase.LOCKING, QrApertureModel.phase(true, 0, wrong = false, found = false))
        assertEquals(QrApertureModel.Phase.READING, QrApertureModel.phase(true, 3, wrong = false, found = false))
        assertEquals(QrApertureModel.Phase.WRONG, QrApertureModel.phase(true, 0, wrong = true, found = false))
        assertEquals(QrApertureModel.Phase.FOUND, QrApertureModel.phase(true, 8, wrong = false, found = true))
    }
}
