package com.localghost.app.local

import android.content.Context
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.io.ByteArrayOutputStream
import java.io.File

/**
 * THE PHONE'S MODEL. Two jobs now: the lifeboat (the box cannot be reached: the phone answers,
 * web findings included, by itself) and the reader (the box is slow , on its CPU, or the model
 * far away , so the phone reads the web pages into notes and the box writes the answer from
 * them). It has no access to the life-index; that lives on the box.
 *
 * One call at a time (a Mutex around the native handle), every call starting from an empty
 * context, speed measured on every call and remembered (so the reader can size its work before
 * it starts), the weights dropped after a few idle minutes (2 GB is not something to hold in a
 * backgrounded app), the thought channel and turn markers stripped from what comes back.
 */
object LocalModel {

    enum class State { ABSENT, NOT_BUILT, NOT_LOADED, LOADING, READY, FAILED }

    @Volatile var state: State = State.ABSENT
        private set
    /** The prompt format the model was driven with last ("gemma4", "model-template", …), for the settings line. */
    @Volatile var lastFormat: String = ""
        private set

    private val native = NativeLlama()
    @Volatile private var handle: Long = 0L
    private val lock = Mutex()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private var idleJob: Job? = null
    private const val IDLE_UNLOAD_MS = 4 * 60_000L
    const val N_CTX = 4096

    /** The active GGUF in app storage (downloaded from the box, never bundled). Null if none installed. */
    fun modelFile(ctx: Context): File? = ModelStore.activeFile(ctx)

    fun isModelPresent(ctx: Context): Boolean = modelFile(ctx) != null

    /** Whether the phone can run a model at all: this APK carries the runtime AND a model is installed. */
    fun usable(ctx: Context): Boolean = NativeLlama.ensureLibrary() && isModelPresent(ctx)

    /** Idempotent load. Returns true when READY. */
    suspend fun ensureLoaded(ctx: Context): Boolean = withContext(Dispatchers.Default) {
        if (state == State.READY && handle != 0L) return@withContext true
        if (!NativeLlama.ensureLibrary()) { state = State.NOT_BUILT; return@withContext false }
        val file = modelFile(ctx) ?: run { state = State.ABSENT; return@withContext false }
        lock.withLock {
            if (handle != 0L) return@withLock
            state = State.LOADING
            // the big cores: on an 8-core phone four are performance cores; more threads than
            // big cores makes llama.cpp slower, not faster
            val threads = (Runtime.getRuntime().availableProcessors() / 2).coerceIn(2, 4)
            val t0 = System.currentTimeMillis()
            handle = native.nativeLoad(file.absolutePath, N_CTX, threads)
            state = if (handle != 0L) State.READY else State.FAILED
            if (handle != 0L) Speed.noteLoad(ctx, System.currentTimeMillis() - t0)
        }
        armIdle()
        state == State.READY
    }

    /** The answer to one system + user message, whole. [onText] sees it grow; return false to stop. */
    class Result(val text: String, val promptTokens: Int, val promptMs: Long, val genTokens: Int, val genMs: Long, val cutTokens: Int, val format: String)

    suspend fun complete(ctx: Context, system: String, user: String, maxTokens: Int = 256, temperature: Float = 0.2f,
                         onText: (String) -> Boolean = { true }): Result? {
        if (!ensureLoaded(ctx)) return null
        return withContext(Dispatchers.Default) {
            lock.withLock {
                val h = handle
                if (h == 0L) return@withLock null
                val buf = ByteArrayOutputStream()
                val format = native.nativeChat(h, system, user, maxTokens, temperature) { bytes ->
                    buf.write(bytes)
                    onText(clean(buf.toString(Charsets.UTF_8.name())))
                }
                if (format.isEmpty()) return@withLock null
                lastFormat = format
                val st = native.nativeStats(h)
                val r = Result(clean(buf.toString(Charsets.UTF_8.name())), st[0].toInt(), st[1], st[2].toInt(), st[3], st[4].toInt(), format)
                Speed.note(ctx, r.promptTokens, r.promptMs, r.genTokens, r.genMs)
                r
            }
        }.also { armIdle() }
    }

    /** The lifeboat's streaming chat: the reply grows piece by piece. [shouldContinue] is the STOP button. */
    fun generate(ctx: Context, prompt: String, maxTokens: Int = 512, system: String = LIFEBOAT_SYSTEM, shouldContinue: () -> Boolean): Flow<String> =
        callbackFlow {
            var sent = 0
            complete(ctx, system, prompt, maxTokens, 0.6f) { whole ->
                if (whole.length > sent) { trySend(whole.substring(sent)); sent = whole.length }
                shouldContinue()
            }
            close()
            awaitClose { }
        }.flowOn(Dispatchers.Default)

    const val LIFEBOAT_SYSTEM = "You are LocalGhost's small on-phone model, answering while the person's own server cannot be reached. " +
        "You cannot see their photos, notes or history. Answer plainly and briefly, and say so when you do not know."

    fun unload() {
        idleJob?.cancel()
        scope.launch {
            lock.withLock {
                if (handle != 0L) { native.nativeFree(handle); handle = 0L }
                state = if (state == State.ABSENT || state == State.NOT_BUILT) state else State.NOT_LOADED
            }
        }
    }

    private fun armIdle() {
        idleJob?.cancel()
        idleJob = scope.launch {
            delay(IDLE_UNLOAD_MS)
            lock.withLock {
                if (handle != 0L) { native.nativeFree(handle); handle = 0L; state = State.NOT_LOADED }
            }
        }
    }

    /** What the model said, without its machinery: a thought channel (Gemma 4 when thinking leaks),
     *  turn and channel markers, and leading blank lines. */
    fun clean(s: String): String {
        var t = s
        // a thought block, closed or still open at the end
        t = t.replace(Regex("<\\|channel>thought.*?<channel\\|>", RegexOption.DOT_MATCHES_ALL), "")
        val open = t.indexOf("<|channel>")
        if (open >= 0) t = t.substring(0, open)
        t = t.replace(Regex("<\\|?(turn|channel|think|end_of_turn|start_of_turn)[^>]*>"), "")
            .replace("<end_of_turn>", "").replace("<start_of_turn>", "")
        return t.trimStart()
    }

    /**
     * THE PHONE'S MEASURED SPEED, remembered across runs: prompt and generation tokens per second as
     * a moving average of real calls. The reader sizes its work from it before it starts; until
     * the phone has measured anything, a conservative guess (a 2B model on a phone CPU).
     */
    object Speed {
        private const val PREFS = "lg_local_speed"
        const val GUESS_PROMPT_TPS = 60.0
        const val GUESS_GEN_TPS = 12.0
        private fun prefs(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

        fun promptTps(ctx: Context): Double = prefs(ctx).getFloat("p", 0f).toDouble().takeIf { it > 0 } ?: GUESS_PROMPT_TPS
        fun genTps(ctx: Context): Double = prefs(ctx).getFloat("g", 0f).toDouble().takeIf { it > 0 } ?: GUESS_GEN_TPS
        fun measured(ctx: Context): Boolean = prefs(ctx).getFloat("p", 0f) > 0f
        fun loadMs(ctx: Context): Long = prefs(ctx).getLong("load", 0L)

        internal fun note(ctx: Context, pTok: Int, pMs: Long, gTok: Int, gMs: Long) {
            val p = prefs(ctx)
            val e = p.edit()
            // only calls long enough to mean something; the first one sets, the rest average in
            if (pTok >= 64 && pMs > 0) e.putFloat("p", ewma(p.getFloat("p", 0f), pTok * 1000f / pMs))
            if (gTok >= 16 && gMs > 0) e.putFloat("g", ewma(p.getFloat("g", 0f), gTok * 1000f / gMs))
            e.apply()
        }

        internal fun noteLoad(ctx: Context, ms: Long) = prefs(ctx).edit().putLong("load", ms).apply()

        fun ewma(old: Float, new: Float): Float = if (old <= 0f) new else old * 0.7f + new * 0.3f

        /** Seconds to read [promptTokens] and write [genTokens] at the measured speed. */
        fun seconds(ctx: Context, promptTokens: Int, genTokens: Int): Double =
            promptTokens / promptTps(ctx) + genTokens / genTps(ctx)
    }
}
