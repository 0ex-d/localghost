# Building LocalGhost

See **BUILDING.md**: what to install and how, on Windows (Android Studio) and Linux (command line),
the release build, the llama.cpp pin, and what to do when a build fails.

Two things from this file's earlier text that still hold:

- **SDK levels.** compileSdk 37, targetSdk 36, minSdk 35. We compile against API 37 (some AndroidX
  libraries require it) while targeting 36, so we get the newer APIs without opting into Android 17
  runtime behaviour until we have tested it.
- **Engine and models are separate.** The ENGINE (llama.cpp, compiled into `liblocalghost_llm.so`)
  ships inside the APK and is covered by its signature. The MODELS (GGUF files) are not in the APK.
  The phone downloads them from your box over mTLS, resumable and Wi-Fi only by default, and checks
  each one's SHA-256 against what the box published (ModelVerifier). The on-phone model runs only
  when the box cannot be reached.
