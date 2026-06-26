# Building LocalGhost

Two paths that don't interfere: dev on Windows with Android Studio, releases on Debian.

## Dev , Windows + Android Studio
Open the project, run to a device over USB. Debug builds use the debug keystore and no signing
pipeline. The VERIFY BUILD screen will show the working tree as DIRTY on dev builds, which is
correct, these are not releases.

Requirements: Android Studio, the Android SDK with platform 37 and build-tools 36.0.0, and (for
the on-phone model) the NDK and CMake. Your `local.properties` (with `sdk.dir` and optional dev
`NAS_BASE_URL`/`DEVICE_TOKEN`) stays on the Windows machine and is gitignored.

## SDK levels
compileSdk 37, targetSdk 36, minSdk 35. We compile against API 37 (some AndroidX libraries require
it) while still targeting 36, so we get the newer APIs without opting into Android 17 runtime
behaviour changes until we test for them.

## How the on-phone model works (engine vs models)
Two separate things, delivered separately.

The ENGINE is llama.cpp, compiled from the pinned commit into a native library
(`liblocalghost_llm.so`, linking llama/ggml) and bundled INSIDE the APK. It ships with the app,
is covered by the APK signature, and is what knows how to run a GGUF. No model is bundled.

The MODELS are GGUF files. They are NOT in the APK. The app downloads them at runtime from your
box (ghost serves them; the phone pulls over mTLS, resumable, Wi-Fi-only by default), stores them
in the app's private files dir, and verifies each one's SHA-256 against the hash the box published
before accepting it (see ModelVerifier). The on-phone path runs only when the box is unreachable;
normally inference is on the box.

So shipping the app gives you the ability to RUN models offline; getting a model is a separate
download you control. This keeps the APK small and reproducible (no multi-GB blob baked in) and
keeps model distribution on your own infrastructure.

## The on-phone model , llama.cpp (native)
The app's offline fallback runs a small GGUF locally via llama.cpp. This is our ONLY external
native dependency, and it is pinned, not vendored as a submodule.

- The pin is `LLAMA_CPP_TAG` in `app/src/main/cpp/CMakeLists.txt`, the single source of truth.
  The setup script, build-env provenance, and deploy version check all read it from there.
- CMake fetches llama.cpp at that tag via FetchContent at configure time. No submodule.
- To bump it: change the tag, rebuild, and re-test against the JNI in
  `app/src/main/cpp/llama_jni.cpp` (it targets the current llama.h API: llama_model_load_from_file,
  llama_init_from_model, llama_model_get_vocab, the sampler chain, llama_batch_get_one,
  llama_token_to_piece). An older tag may need JNI tweaks.

Enable the NDK build in `app/build.gradle.kts` (arm64 covers the S26; add x86_64 for the emulator):
```kotlin
android {
    defaultConfig {
        ndk { abiFilters += listOf("arm64-v8a") }
        externalNativeBuild { cmake { arguments += listOf("-DANDROID_STL=c++_shared") } }
    }
    externalNativeBuild {
        cmake { path = file("src/main/cpp/CMakeLists.txt"); version = "3.22.1" }
    }
}
```

## Dependencies
Everything except llama.cpp is AndroidX/Compose, all versions in `gradle/libs.versions.toml`. The
WorkManager dependency is in the catalog (`androidx-work-runtime-ktx`, version `workRuntime`); the
build file references it as `libs.androidx.work.runtime.ktx` like every other dependency.

## Release , Debian (the website box)
Linux is the better release target: more deterministic, and the GPG key + workflow already live
there.

One-time setup:
```
tools/debian_setup.sh          # JDK 21 + cmdline-tools + platform 37 + build-tools 36.0.0,
                               # and pre-clones llama.cpp at the pinned tag
source ~/.localghost_android_env
export LG_KEYSTORE=/path/to/localghost-release.jks
export LG_KEY_ALIAS=localghost
```
JDK note: Debian 13 (Trixie) ships JDK 21 (no openjdk-17 in its main repos). AGP 9.x runs on JDK
21, so that's what the script installs.

Build, sign, publish:
```
tools/release.sh
git push
# create a GitHub release/tag, attach app-release.apk + app-release.apk.asc, and paste:
#   commit, apk sha-256, manifest root, signing cert sha-256
```
`release.sh` checks the tree is clean, writes the build environment (`ghost/build-env.txt`), signs
the source manifest, builds, zipalign + apksigner signs, and GPG-signs the APK. See VERIFY.md for
what gets published and how anyone checks it.

## Reproducibility
The release APK bakes in no machine-specific data (NAS_BASE_URL/DEVICE_TOKEN are empty; the app
reads them from encrypted storage at setup). For byte-identical rebuilds, the toolchain is pinned:
the Gradle wrapper, `gradle/libs.versions.toml`, build-tools 36.0.0, and `LLAMA_CPP_TAG`. The
exact JDK and OS are recorded in `ghost/build-env.txt` so a verifier can match them.
