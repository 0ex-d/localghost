package com.localghost.app.voice

import android.content.Context
import android.media.MediaPlayer
import com.localghost.app.net.BoxHttp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.withContext
import java.io.File

/**
 * Plays one voice note at a time: from the phone when it has not gone to the box yet, else from
 * the box (/v1/voice/audio) into the app's cache, deleted again when playback stops. Tap the same
 * note again to stop.
 */
object VoicePlayback {
    private val _playing = MutableStateFlow<String?>(null)
    val playing: StateFlow<String?> get() = _playing
    private var player: MediaPlayer? = null
    private var cached: File? = null

    /** Play the note, or stop it if it is the one playing. False when the audio could not be had. */
    suspend fun toggle(ctx: Context, id: String): Boolean {
        if (_playing.value == id) { stop(); return true }
        stop()
        val local = VoiceNotes.localFile(ctx, id)
        val f = local ?: File(File(ctx.cacheDir, "voice-play").apply { mkdirs() }, "$id.wav").also { dst ->
            if (!BoxHttp.getToFile(ctx, "/v1/voice/audio?id=$id", dst)) return false
            cached = dst
        }
        return withContext(Dispatchers.Main) {
            try {
                val mp = MediaPlayer()
                mp.setDataSource(f.path)
                mp.setOnCompletionListener { stop() }
                mp.prepare()
                mp.start()
                player = mp
                _playing.value = id
                true
            } catch (e: Exception) {
                android.util.Log.w("LocalGhost", "voice: playback failed: ${e.message}")
                stop()
                false
            }
        }
    }

    fun stop() {
        runCatching { player?.stop() }
        player?.release()
        player = null
        cached?.delete()
        cached = null
        _playing.value = null
    }
}
