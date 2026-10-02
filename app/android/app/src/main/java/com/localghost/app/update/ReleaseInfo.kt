package com.localghost.app.update

/**
 * What the mirror says about server releases, read the way the box reads it (tools/mirror_fetch.sh):
 * MANIFEST.txt is three header lines ("# LocalGhost Mirror Manifest", "# Build: <build>",
 * "# Signed: <time>"), a blank line, then "<sha256>  /<build>/<set>/<file>". The server set holds
 * the release bundle, RELEASE.txt (version, commit, date, the commits since the last tag), its
 * NOTICE.txt and TERMS-*.txt.
 *
 * The phone does not check the manifest's signature: it only decides whether to ask the person,
 * and the box checks everything, with the key it already holds, before it puts a release on. A
 * forged manifest can make the phone offer a release; it cannot make the box take one.
 *
 * Pure, so the tests read it.
 */
object ReleaseInfo {
    /** One file of the server set: its hash, its path on the mirror (/<build>/server/<name>), its name. */
    data class SetFile(val sha256: String, val path: String, val name: String)

    data class Manifest(val build: String, val server: List<SetFile>)

    /** [name]: the release's name ("wisp"), "" for one without. */
    data class Release(val version: String, val commit: String, val date: String, val bundle: String,
                       val since: String, val changes: List<String>, val name: String = "") {
        /** "wisp 0.0.1", or the version alone. */
        val label: String get() = if (name.isNotBlank()) "$name $version" else version
    }

    private val LINE = Regex("^([0-9a-f]{64})  (/([0-9]{8}T[0-9]{6}Z)/server/([A-Za-z0-9][A-Za-z0-9._+-]*))$")

    fun manifest(text: String): Manifest? {
        val lines = text.lines()
        if (lines.firstOrNull() != "# LocalGhost Mirror Manifest") return null
        val build = lines.firstOrNull { it.startsWith("# Build: ") }?.removePrefix("# Build: ")?.trim() ?: return null
        val files = lines.mapNotNull { l -> LINE.matchEntire(l.trim())?.let { m ->
            if (m.groupValues[3] != build) null else SetFile(m.groupValues[1], m.groupValues[2], m.groupValues[4])
        } }
        return Manifest(build, files)
    }

    fun release(text: String): Release? {
        val kv = HashMap<String, String>()
        val changes = ArrayList<String>()
        var inChanges = false
        for (raw in text.lines()) {
            if (inChanges) {
                if (raw.startsWith("  ")) { raw.trim().takeIf { it.isNotEmpty() }?.let { changes.add(it) }; continue }
                inChanges = false
            }
            if (raw.trim() == "changes:") { inChanges = true; continue }
            val i = raw.indexOf('=')
            if (i > 0) kv[raw.substring(0, i).trim()] = raw.substring(i + 1).trim()
        }
        val v = kv["version"]?.takeIf { it.isNotEmpty() } ?: return null
        return Release(v, kv["commit"] ?: "", kv["date"] ?: "", kv["bundle"] ?: "", kv["since"] ?: "", changes, kv["name"] ?: "")
    }

    /**
     * Whether [offered] is newer than what the box runs ([running]: a release version, or a build
     * from source like "v0.9.2-5-gabc1234-dirty", or "dev"). Numbers compared part by part; a
     * build from source counts as its tag plus the commits on top (so the tag itself is not newer,
     * the next one is). Unknown ("dev", "") is always older: the person decides.
     */
    fun newer(offered: String, running: String): Boolean {
        val o = numbers(offered) ?: return false
        val r = numbers(running) ?: return true
        for (i in 0 until maxOf(o.size, r.size)) {
            val a = o.getOrElse(i) { 0 }
            val b = r.getOrElse(i) { 0 }
            if (a != b) return a > b
        }
        return false
    }

    private fun numbers(v: String): List<Int>? {
        val core = v.trim().removePrefix("v").substringBefore('-').substringBefore('+')
        if (core.isEmpty()) return null
        val parts = core.split('.')
        // a bare commit hash (git describe --always, no tag yet) can be all digits: not a version
        if (parts.size == 1 && core.length >= 7) return null
        return parts.map { it.toIntOrNull() ?: return null }
    }
}
