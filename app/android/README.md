# LocalGhost (Android)

The LocalGhost phone client. A stateless, authenticated limb of your box. All your data and
intelligence live on the box; the phone authenticates over mTLS, holds nothing durable, and clears
its in-memory cache the moment it locks. The tagline is the whole design: the only cloud is you.

This repository is public and the builds are verifiable. You can confirm the APK on your phone was
compiled from this exact source, by us, and signed with our key. See VERIFY.md.

## What's here
- Kotlin + Jetpack Compose app. Package `com.localghost.app`. Portrait, single-activity.
- A single `BoxClient` seam (net/BoxClient.kt) is the boundary to the box. Most of the app talks to
  the box through it; the box daemons (ghost.secd and friends) are a separate Go codebase.
- An on-phone fallback model via llama.cpp (native), used only when the box is unreachable.

## Layout
- `app/src/main/java/com/localghost/app/` , the app (ui/, net/, security/, local/, sync/, notify/).
- `app/src/main/cpp/` , the JNI + CMake for the on-phone llama.cpp fallback.
- `app/src/test/` , JVM unit tests (the lock decision and the model integrity check).
- `tools/` , the Debian setup and release scripts, and the source-signing scripts.
- `ghost/` , provenance: signed source manifest and build environment (which records the pinned llama.cpp tag).

## Build
See BUILDING.md: what to install and how on Windows (Android Studio) and Linux (command line),
releases, and fixes for the errors a build can hit.
- Day to day: Android Studio on Windows, run to the device.
- Releases: built on Debian (the same box as the website), signed, and published with hashes. See
  BUILDING.md and VERIFY.md.

## External dependencies
One, on the native side: llama.cpp. It comes as a source tarball from the LocalGhost mirror (the
same one the box builds from), pinned by SHA-256 in `app/src/main/cpp/CMakeLists.txt`
(`LLAMA_CPP_SHA256`), and is fetched at build time with no submodule and nothing from GitHub.
Everything else is AndroidX/Compose via the version catalog (`gradle/libs.versions.toml`).

## Tests
`./gradlew test` , JVM, no device. Two focused suites: the lock-on-background decision (AuthGate)
and the downloaded-model SHA-256 check (ModelVerifier). The security-critical persona/WIPE
invariants live in the box (ghost.secd) and are tested there, not here.

## Setup and enrollment
First launch shows the setup screen (SetupScreen): enter the box address (typed or QR), a
one-time pairing code, and a device name, then enrol. On success the box connection details are
written to encrypted local storage (security/BoxConfig) and the app moves to the lock gate. The
"is BoxConfig populated" check is the setup-vs-use signal. The release APK ships unconfigured, so
every fresh install starts at setup.

## Status
Pre-release. The box side (ghost.secd) is the remaining keystone. The seam is BoxClient: every
method there is stubbed, including `enroll()` (shaped for the real mTLS CSR + pairing-code
exchange) and the QR scanner hook in setup. Pointing the app at a real box means implementing
ghost.secd and making those BoxClient calls real, no UI changes needed.
