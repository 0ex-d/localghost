package com.localghost.app.local

/**
 * Raw JNI binding to llama.cpp, one to one with llama_jni.cpp. Not used directly by the app:
 * LocalModel wraps it with a lock, streaming, speed accounting and the model's lifecycle.
 */
internal class NativeLlama {
    /** Generated text as UTF-8 bytes (whole characters only). Return false to stop generating. */
    fun interface ByteCallback { fun onBytes(bytes: ByteArray): Boolean }

    external fun nativeLoad(modelPath: String, nCtx: Int, nThreads: Int): Long
    external fun nativeFree(handle: Long)
    /** One system + user message in the model's own turn format; returns the format used ("" on failure). */
    external fun nativeChat(handle: Long, system: String, user: String, maxTokens: Int, temperature: Float, callback: ByteCallback): String
    /** The last call: [prompt tokens, prompt ms, generated tokens, generation ms, prompt tokens cut to fit]. */
    external fun nativeStats(handle: Long): LongArray

    companion object {
        @Volatile private var loaded = false
        /** Loads liblocalghost_llm.so; false when this APK was built without it (no llama.cpp pin). */
        fun ensureLibrary(): Boolean = try {
            if (!loaded) { System.loadLibrary("localghost_llm"); loaded = true }
            true
        } catch (e: Throwable) { false }
    }
}
