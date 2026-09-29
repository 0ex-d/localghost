// The phone's model: llama.cpp behind a JNI surface small enough to get right.
//
//   nativeLoad(path, nCtx, nThreads)            -> handle (0 on failure)
//   nativeFree(handle)
//   nativeChat(handle, system, user, maxTokens, temperature, callback) -> the prompt format used
//   nativeStats(handle)                         -> [promptTokens, promptMs, genTokens, genMs, truncated]
//
// What the first version got wrong, and this one does not:
//  - the KV cache was never cleared, so the second question was decoded after the first one's
//    tokens and the context filled up within a few turns: every call now starts from an empty
//    memory (llama_memory_clear);
//  - the whole prompt went to llama_decode in one batch, which fails past n_batch (a web page is
//    thousands of tokens): prefill is chunked by n_batch;
//  - a prompt longer than the context was undefined behaviour: it is cut to fit, the head kept,
//    and the cut is reported;
//  - pieces went to Java through NewStringUTF, which requires modified UTF-8 and ABORTS the process
//    on a multi-byte character split across two tokens or on any 4-byte character (emoji): pieces
//    now go as bytes, and only whole UTF-8 sequences are sent;
//  - no chat template was applied, so the model saw raw text: Gemma 4's turns are applied when
//    the vocabulary has them (llama.cpp's built-in template list does not know Gemma 4's
//    "<|turn>" format), else the model's own template through llama.cpp, else Gemma's classic one;
//  - llama_backend_init/free ran per model: once per process now.

#include <jni.h>
#include <android/log.h>
#include <algorithm>
#include <cstring>
#include <mutex>
#include <string>
#include <vector>
#include "llama.h"

#define LOG_TAG "localghost_llm"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

namespace {

std::once_flag g_backend_once;

struct LgLlm {
    llama_model*       model = nullptr;
    llama_context*     ctx   = nullptr;
    const llama_vocab* vocab = nullptr;
    std::mutex         mu;   // one generation at a time per handle
    // the last call's numbers
    int32_t last_prompt = 0, last_gen = 0, last_truncated = 0;
    double  last_prompt_ms = 0, last_gen_ms = 0;
};

std::string jstr(JNIEnv* env, jstring s) {
    if (!s) return {};
    // GetStringUTFChars is modified UTF-8; go through the UTF-16 chars and encode real UTF-8 so a
    // page with an emoji in it is not silently mangled on the way in.
    const jsize n = env->GetStringLength(s);
    const jchar* c = env->GetStringChars(s, nullptr);
    std::string out;
    out.reserve(n);
    for (jsize i = 0; i < n; i++) {
        uint32_t cp = c[i];
        if (cp >= 0xD800 && cp <= 0xDBFF && i + 1 < n && c[i + 1] >= 0xDC00 && c[i + 1] <= 0xDFFF) {
            cp = 0x10000 + ((cp - 0xD800) << 10) + (c[i + 1] - 0xDC00);
            i++;
        } else if (cp >= 0xD800 && cp <= 0xDFFF) {
            cp = 0xFFFD; // a lone surrogate
        }
        if (cp < 0x80) out.push_back((char) cp);
        else if (cp < 0x800) { out.push_back((char) (0xC0 | (cp >> 6))); out.push_back((char) (0x80 | (cp & 0x3F))); }
        else if (cp < 0x10000) { out.push_back((char) (0xE0 | (cp >> 12))); out.push_back((char) (0x80 | ((cp >> 6) & 0x3F))); out.push_back((char) (0x80 | (cp & 0x3F))); }
        else { out.push_back((char) (0xF0 | (cp >> 18))); out.push_back((char) (0x80 | ((cp >> 12) & 0x3F))); out.push_back((char) (0x80 | ((cp >> 6) & 0x3F))); out.push_back((char) (0x80 | (cp & 0x3F))); }
    }
    env->ReleaseStringChars(s, c);
    return out;
}

// complete_utf8_prefix is how many leading bytes of buf form whole UTF-8 sequences; the rest waits
// for the next piece.
size_t complete_utf8_prefix(const std::string& buf) {
    size_t n = buf.size();
    // walk back at most 3 bytes to find a lead byte whose sequence is not finished
    for (size_t back = 1; back <= 4 && back <= n; back++) {
        unsigned char c = (unsigned char) buf[n - back];
        if ((c & 0xC0) == 0x80) continue; // continuation byte
        size_t need = (c & 0x80) == 0 ? 1 : (c & 0xE0) == 0xC0 ? 2 : (c & 0xF0) == 0xE0 ? 3 : (c & 0xF8) == 0xF0 ? 4 : 1;
        return back >= need ? n : n - back;
    }
    return n;
}

// single_special reports whether text tokenizes to exactly one token (a special token the
// vocabulary knows), which is how the Gemma 4 turn format is recognised.
bool single_special(const llama_vocab* vocab, const char* text) {
    llama_token t[4];
    int32_t n = llama_tokenize(vocab, text, (int32_t) strlen(text), t, 4, false, true);
    return n == 1;
}

// build_prompt renders the conversation (one system and one user message) in the model's format.
// Returns the format's name for the log and the caller.
std::string build_prompt(LgLlm* h, const std::string& system, const std::string& user, std::string& prompt) {
    // 1. Gemma 4: <|turn>system\n…<turn|>\n<|turn>user\n…<turn|>\n<|turn>model\n (thinking off:
    //    no <|think|> in the system turn). BOS comes from the tokenizer (add_special).
    if (single_special(h->vocab, "<|turn>") && single_special(h->vocab, "<turn|>")) {
        prompt.clear();
        if (!system.empty()) prompt += "<|turn>system\n" + system + "<turn|>\n";
        prompt += "<|turn>user\n" + user + "<turn|>\n<|turn>model\n";
        return "gemma4";
    }
    // 2. the model's own template, when llama.cpp's built-in list knows it
    std::vector<llama_chat_message> msgs;
    if (!system.empty()) msgs.push_back({"system", system.c_str()});
    msgs.push_back({"user", user.c_str()});
    const char* tmpl = llama_model_chat_template(h->model, nullptr);
    const char* names[2] = {tmpl, "gemma"};
    for (int k = 0; k < 2; k++) {
        if (!names[k]) continue;
        int32_t need = llama_chat_apply_template(names[k], msgs.data(), msgs.size(), true, nullptr, 0);
        if (need <= 0) continue;
        std::vector<char> buf(need + 1);
        int32_t got = llama_chat_apply_template(names[k], msgs.data(), msgs.size(), true, buf.data(), (int32_t) buf.size());
        if (got <= 0) continue;
        prompt.assign(buf.data(), std::min<int32_t>(got, need));
        return k == 0 ? "model-template" : "gemma-classic";
    }
    // 3. nothing: the text as it is
    prompt = system.empty() ? user : system + "\n\n" + user;
    return "raw";
}

// use_mmap left llama_model_params in the mirror's v0.5.0 (the same release dropped --mlock from
// llama-server). Asked for where the field exists, left to the library's own loading where it does
// not, so the bridge builds against either side of that change.
template <typename P>
auto prefer_mmap(P& p, int) -> decltype(p.use_mmap = true, void()) { p.use_mmap = true; }
template <typename P>
void prefer_mmap(P&, long) {}

bool is_stop_piece(const std::string& p) {
    return p == "<turn|>" || p == "<end_of_turn>" || p == "<|im_end|>" || p == "<|eot_id|>";
}

} // namespace

extern "C" JNIEXPORT jlong JNICALL
Java_com_localghost_app_local_NativeLlama_nativeLoad(JNIEnv* env, jobject, jstring jModelPath, jint nCtx, jint nThreads) {
    std::call_once(g_backend_once, [] { llama_backend_init(); });
    const std::string path = jstr(env, jModelPath);

    llama_model_params mparams = llama_model_default_params();
    mparams.n_gpu_layers = 0; // CPU: no GPU backend is compiled into the phone build
    prefer_mmap(mparams, 0);  // the weights page in from flash as needed; Android can reclaim them
    llama_model* model = llama_model_load_from_file(path.c_str(), mparams);
    if (!model) { LOGE("model load failed: %s", path.c_str()); return 0; }

    llama_context_params cparams = llama_context_default_params();
    int32_t train = llama_model_n_ctx_train(model);
    int32_t want = nCtx > 0 ? nCtx : 4096;
    if (train > 0 && want > train) want = train;
    cparams.n_ctx           = (uint32_t) want;
    cparams.n_batch         = 512;
    cparams.n_ubatch        = 512;
    cparams.n_threads       = nThreads;
    cparams.n_threads_batch = nThreads;
    cparams.no_perf         = false;
    llama_context* ctx = llama_init_from_model(model, cparams);
    if (!ctx) { LOGE("context init failed (n_ctx %d)", want); llama_model_free(model); return 0; }

    auto* h = new LgLlm();
    h->model = model;
    h->ctx = ctx;
    h->vocab = llama_model_get_vocab(model);
    LOGI("model loaded: n_ctx %u, threads %d", llama_n_ctx(ctx), nThreads);
    return reinterpret_cast<jlong>(h);
}

extern "C" JNIEXPORT void JNICALL
Java_com_localghost_app_local_NativeLlama_nativeFree(JNIEnv*, jobject, jlong handle) {
    auto* h = reinterpret_cast<LgLlm*>(handle);
    if (!h) return;
    {
        std::lock_guard<std::mutex> lock(h->mu); // never free under a running generation
        if (h->ctx) llama_free(h->ctx);
        if (h->model) llama_model_free(h->model);
        h->ctx = nullptr;
        h->model = nullptr;
    }
    delete h;
}

// nativeChat streams the answer to one system + user message through callback.onBytes(byte[]):
// Boolean (return false to stop). Returns the prompt format used, or "" on failure.
extern "C" JNIEXPORT jstring JNICALL
Java_com_localghost_app_local_NativeLlama_nativeChat(JNIEnv* env, jobject, jlong handle, jstring jSystem, jstring jUser,
                                                     jint maxTokens, jfloat temperature, jobject callback) {
    auto* h = reinterpret_cast<LgLlm*>(handle);
    if (!h || !h->ctx) return env->NewStringUTF("");
    std::lock_guard<std::mutex> lock(h->mu);

    jclass cbClass = env->GetObjectClass(callback);
    jmethodID onBytes = env->GetMethodID(cbClass, "onBytes", "([B)Z");
    if (!onBytes) { LOGE("callback has no onBytes([B)Z"); return env->NewStringUTF(""); }

    std::string prompt;
    const std::string format = build_prompt(h, jstr(env, jSystem), jstr(env, jUser), prompt);

    // a fresh memory for every call: the phone's model answers one thing at a time
    llama_memory_clear(llama_get_memory(h->ctx), true);
    llama_perf_context_reset(h->ctx);
    h->last_prompt = h->last_gen = h->last_truncated = 0;
    h->last_prompt_ms = h->last_gen_ms = 0;

    // tokenize (BOS from the vocabulary, special tokens in the template parsed as such)
    int32_t n = -llama_tokenize(h->vocab, prompt.c_str(), (int32_t) prompt.size(), nullptr, 0, true, true);
    if (n <= 0) { LOGE("tokenize failed"); return env->NewStringUTF(""); }
    std::vector<llama_token> tokens(n);
    if (llama_tokenize(h->vocab, prompt.c_str(), (int32_t) prompt.size(), tokens.data(), n, true, true) < 0) {
        LOGE("tokenize failed"); return env->NewStringUTF("");
    }
    // fit the context: prompt + answer + a little room. The head is kept and the TAIL of the
    // prompt, the generation cue, is re-appended, so a cut page still ends in "<|turn>model\n".
    const int32_t n_ctx = (int32_t) llama_n_ctx(h->ctx);
    int32_t max_gen = maxTokens > 0 ? maxTokens : 256;
    if (max_gen > n_ctx / 2) max_gen = n_ctx / 2;
    const int32_t room = n_ctx - max_gen - 8;
    if ((int32_t) tokens.size() > room) {
        const int32_t keep_tail = 16;
        std::vector<llama_token> cut(tokens.begin(), tokens.begin() + (room - keep_tail));
        cut.insert(cut.end(), tokens.end() - keep_tail, tokens.end());
        h->last_truncated = (int32_t) tokens.size() - (int32_t) cut.size();
        tokens.swap(cut);
    }

    // prefill in n_batch chunks
    const int32_t n_batch = (int32_t) llama_n_batch(h->ctx);
    for (int32_t i = 0; i < (int32_t) tokens.size(); i += n_batch) {
        int32_t len = std::min<int32_t>(n_batch, (int32_t) tokens.size() - i);
        if (llama_decode(h->ctx, llama_batch_get_one(tokens.data() + i, len)) != 0) {
            LOGE("prefill failed at %d of %zu", i, tokens.size());
            return env->NewStringUTF("");
        }
    }

    llama_sampler* smpl = llama_sampler_chain_init(llama_sampler_chain_default_params());
    if (temperature <= 0.0f) {
        llama_sampler_chain_add(smpl, llama_sampler_init_greedy());
    } else {
        llama_sampler_chain_add(smpl, llama_sampler_init_top_k(40));
        llama_sampler_chain_add(smpl, llama_sampler_init_top_p(0.95f, 1));
        llama_sampler_chain_add(smpl, llama_sampler_init_temp(temperature));
        llama_sampler_chain_add(smpl, llama_sampler_init_dist(LLAMA_DEFAULT_SEED));
    }

    std::string pending; // bytes not yet sent: an unfinished UTF-8 sequence
    char piece[256];
    int32_t used = (int32_t) tokens.size();
    for (int32_t g = 0; g < max_gen && used < n_ctx; g++) {
        llama_token id = llama_sampler_sample(smpl, h->ctx, -1);
        if (llama_vocab_is_eog(h->vocab, id)) break;
        int32_t pn = llama_token_to_piece(h->vocab, id, piece, sizeof(piece), 0, true);
        if (pn < 0) break;
        std::string p(piece, pn);
        if (is_stop_piece(p)) break;
        pending += p;
        size_t ready = complete_utf8_prefix(pending);
        if (ready > 0) {
            jbyteArray arr = env->NewByteArray((jsize) ready);
            env->SetByteArrayRegion(arr, 0, (jsize) ready, reinterpret_cast<const jbyte*>(pending.data()));
            jboolean cont = env->CallBooleanMethod(callback, onBytes, arr);
            env->DeleteLocalRef(arr);
            pending.erase(0, ready);
            if (env->ExceptionCheck() || !cont) break;
        }
        if (llama_decode(h->ctx, llama_batch_get_one(&id, 1)) != 0) { LOGE("decode failed"); break; }
        used++;
    }
    llama_sampler_free(smpl);

    llama_perf_context_data perf = llama_perf_context(h->ctx);
    h->last_prompt = perf.n_p_eval;
    h->last_prompt_ms = perf.t_p_eval_ms;
    h->last_gen = perf.n_eval;
    h->last_gen_ms = perf.t_eval_ms;
    return env->NewStringUTF(format.c_str());
}

// nativeStats: the last call's [prompt tokens, prompt ms, generated tokens, generation ms, tokens cut].
extern "C" JNIEXPORT jlongArray JNICALL
Java_com_localghost_app_local_NativeLlama_nativeStats(JNIEnv* env, jobject, jlong handle) {
    auto* h = reinterpret_cast<LgLlm*>(handle);
    jlong v[5] = {0, 0, 0, 0, 0};
    if (h) {
        v[0] = h->last_prompt;
        v[1] = (jlong) h->last_prompt_ms;
        v[2] = h->last_gen;
        v[3] = (jlong) h->last_gen_ms;
        v[4] = h->last_truncated;
    }
    jlongArray out = env->NewLongArray(5);
    env->SetLongArrayRegion(out, 0, 5, v);
    return out;
}
