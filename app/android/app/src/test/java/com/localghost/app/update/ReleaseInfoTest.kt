package com.localghost.app.update

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ReleaseInfoTest {
    private val manifest = """# LocalGhost Mirror Manifest
# Build: 20260930T120000Z
# Signed: 2026-09-30T12:00:00Z

fa8a3dbf0e91040949414b86cf5a67b9971c6ee79c18c95e55410d21b2d888ff  /20260930T120000Z/server/NOTICE.txt
8f8a92015825604a3f73c0d2dafd4139c2e115706476cbc64a3ef467916071b1  /20260930T120000Z/server/RELEASE.txt
a23072b4dd44e7c82758696f242d74d8c51bac6895bd4907fa05ef72ed8c8ccd  /20260930T120000Z/server/localghost-server-0.9.3-linux-amd64.tar.gz
b23072b4dd44e7c82758696f242d74d8c51bac6895bd4907fa05ef72ed8c8ccd  /20260930T120000Z/geo/allCountries.zip
c23072b4dd44e7c82758696f242d74d8c51bac6895bd4907fa05ef72ed8c8ccd  /20260901T000000Z/server/old.tar.gz
d23072b4dd44e7c82758696f242d74d8c51bac6895bd4907fa05ef72ed8c8ccd  /20260930T120000Z/server/../../etc/passwd
"""

    @Test fun readsTheServerSetOfTheCurrentBuild() {
        val m = ReleaseInfo.manifest(manifest)!!
        assertEquals("20260930T120000Z", m.build)
        assertEquals(listOf("NOTICE.txt", "RELEASE.txt", "localghost-server-0.9.3-linux-amd64.tar.gz"), m.server.map { it.name })
        assertEquals("/20260930T120000Z/server/RELEASE.txt", m.server[1].path)
        assertNull(ReleaseInfo.manifest("not a manifest"))
    }

    @Test fun readsTheReleaseNotes() {
        val r = ReleaseInfo.release("version=0.9.3\ncommit=e830701\ndate=2026-09-30T11:17:32Z\nbundle=b.tar.gz\nsince=v0.9.2\nchanges:\n  e830701 the vault rings\n  a1b2c3d trail questions\n")!!
        assertEquals("0.9.3", r.version)
        assertEquals(listOf("e830701 the vault rings", "a1b2c3d trail questions"), r.changes)
        assertNull(ReleaseInfo.release("commit=x"))
    }

    @Test fun comparesVersions() {
        assertTrue(ReleaseInfo.newer("0.9.3", "0.9.2"))
        assertTrue(ReleaseInfo.newer("0.10.0", "0.9.9"))
        assertFalse(ReleaseInfo.newer("0.9.3", "0.9.3"))
        assertFalse(ReleaseInfo.newer("0.9.2", "0.9.3"))
        // a build from source: its tag and the commits on top
        assertFalse(ReleaseInfo.newer("0.9.2", "v0.9.2-5-gabc1234-dirty"))
        assertTrue(ReleaseInfo.newer("0.9.3", "v0.9.2-5-gabc1234"))
        assertTrue(ReleaseInfo.newer("0.9.3", "dev"))
        assertTrue(ReleaseInfo.newer("0.9.3", "abc1234"))   // git describe --always, no tag yet
        assertTrue(ReleaseInfo.newer("0.9.3", "1234567"))   // a hash that happens to be all digits
        assertFalse(ReleaseInfo.newer("nonsense", "0.9.2"))
    }
}
