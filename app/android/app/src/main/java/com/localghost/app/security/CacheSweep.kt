package com.localghost.app.security

import java.io.File

/**
 * WHAT THE APP LEFT IN ITS CACHE (free-time notes, 30 Sep 2026). A photo taken from the chat
 * (capture_*.jpg) was uploaded and then kept; a video that fell back to a download (play-*.tmp)
 * or a voice note fetched to play (voice-play/) is deleted when it closes, but not when the app is
 * killed while it plays; the data export (localghost-export.json) was never deleted. All of it is
 * the box's content, plain, in the phone's storage. Swept when the app locks and when it starts.
 * Pure over a directory, so the rule is tested.
 */
object CacheSweep {
    private val patterns = listOf(Regex("""capture_\d+\.jpg"""), Regex("""play-[0-9a-f]+\.tmp"""),
        Regex("""localghost-export\.json"""))

    /** Deletes the leftovers in [cacheDir] older than [minAgeMs] (0: all); returns how many. */
    fun sweep(cacheDir: File, minAgeMs: Long = 0, now: Long = System.currentTimeMillis()): Int {
        var n = 0
        fun old(f: File) = minAgeMs <= 0 || now - f.lastModified() >= minAgeMs
        cacheDir.listFiles()?.forEach { f ->
            if (f.isFile && patterns.any { it.matches(f.name) } && old(f) && f.delete()) n++
        }
        File(cacheDir, "voice-play").listFiles()?.forEach { f -> if (f.isFile && old(f) && f.delete()) n++ }
        return n
    }
}
