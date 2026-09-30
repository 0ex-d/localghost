package com.localghost.app.sync

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

// The spool line carries how a point was taken as its fifth field; older lines still read.
class TrailLineTest {
    @Test fun formatsAndReadsEveryShape() {
        assertEquals("1790000000 39.2 20.1", TrailLine.format(1790000000, 39.2, 20.1, 0f, ""))
        assertEquals("1790000000 39.2 20.1 12", TrailLine.format(1790000000, 39.2, 20.1, 12.7f, ""))
        assertEquals("1790000000 39.2 20.1 0 p", TrailLine.format(1790000000, 39.2, 20.1, 0f, "p"))
        assertEquals(TrailLine.Fields(1790000000, 39.2, 20.1, 0f, ""), TrailLine.parse("1790000000 39.2 20.1"))
        assertEquals(TrailLine.Fields(1790000000, 39.2, 20.1, 12f, ""), TrailLine.parse("1790000000 39.2 20.1 12"))
        assertEquals(TrailLine.Fields(1790000000, 39.2, 20.1, 35f, "w"), TrailLine.parse(TrailLine.format(1790000000, 39.2, 20.1, 35f, "w")))
        assertNull(TrailLine.parse("1790000000 39.2"))
        assertNull(TrailLine.parse("1790000000 39.2 20.1 0 p x"))
    }
}
