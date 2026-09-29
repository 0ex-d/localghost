#!/usr/bin/env bash
# One-time setup on Debian 13 / Ubuntu 24.04 to build the LocalGhost APK headlessly (no Android
# Studio). Installs JDK 21, the Android command-line tools, and the exact SDK packages this project
# uses: platforms 37 and 36, build-tools 36.0.0, platform-tools (adb), NDK 28.2.13676358 and
# CMake 3.22.1 (the last two build the phone model's runtime). Writes ~/.localghost_android_env and,
# when missing, local.properties with sdk.dir. See BUILDING.md.
set -euo pipefail

ANDROID_HOME="${ANDROID_HOME:-$HOME/android-sdk}"
CMDLINE_VER="latest"

# The Android Gradle Plugin needs JDK 17+ (Gradle runs on any JVM 17..26). Debian 13 (Trixie)
# dropped openjdk-17 from its main repos and ships JDK 21 (default) and 25. JDK 21 is the right
# choice here: it is in the default repo and AGP 9.x runs on it. (If you specifically need 17,
# use the Adoptium Temurin repo and temurin-17-jdk instead.)
echo "> JDK 21 (Trixie default, runs the Android Gradle Plugin)..."
if ! command -v javac >/dev/null || ! javac -version 2>&1 | grep -qE 'javac (1[7-9]|2[0-6])'; then
    sudo apt-get update
    sudo apt-get install -y openjdk-21-jdk-headless unzip wget
fi
# Resolve JAVA_HOME from javac so it points at the real JDK (not a /usr/bin symlink dir).
export JAVA_HOME="$(dirname "$(dirname "$(readlink -f "$(command -v javac)")")")"
echo "  JAVA_HOME=$JAVA_HOME"
javac -version

echo "> Android command-line tools..."
mkdir -p "$ANDROID_HOME/cmdline-tools"
if [ ! -d "$ANDROID_HOME/cmdline-tools/$CMDLINE_VER" ]; then
    # Latest cmdline-tools zip. If this URL 404s, get the current one from
    # https://developer.android.com/studio#command-line-tools-only and replace it.
    TOOLS_ZIP="commandlinetools-linux-13114758_latest.zip"
    wget -q "https://dl.google.com/android/repository/$TOOLS_ZIP" -O /tmp/cmdtools.zip
    unzip -q /tmp/cmdtools.zip -d /tmp/cmdtools
    mv /tmp/cmdtools/cmdline-tools "$ANDROID_HOME/cmdline-tools/$CMDLINE_VER"
fi

export PATH="$ANDROID_HOME/cmdline-tools/$CMDLINE_VER/bin:$ANDROID_HOME/platform-tools:$PATH"

echo "> Accepting licenses + installing SDK packages this project needs..."
yes | sdkmanager --sdk_root="$ANDROID_HOME" --licenses >/dev/null
# Note: API 37 installs as "platforms;android-37.0" (not android-37). compileSdk = release(37)
# resolves against it. If a build ever fails "looking for android-37", that .0 naming is why.
# The NDK and CMake versions are the ones app/build.gradle.kts pins (ndkVersion, cmake version).
sdkmanager --sdk_root="$ANDROID_HOME" \
    "platform-tools" \
    "platforms;android-37.0" \
    "platforms;android-36" \
    "build-tools;36.0.0" \
    "ndk;28.2.13676358" \
    "cmake;3.22.1"

# Persist env for future shells.
PROFILE="$HOME/.localghost_android_env"
cat > "$PROFILE" <<ENV
export JAVA_HOME="$JAVA_HOME"
export ANDROID_HOME="$ANDROID_HOME"
export PATH="\$ANDROID_HOME/cmdline-tools/$CMDLINE_VER/bin:\$ANDROID_HOME/platform-tools:\$ANDROID_HOME/build-tools/36.0.0:\$PATH"
ENV
echo
echo "Done. Add this to your shell rc (or 'source' it before building):"
echo "    source $PROFILE"
echo
# --- llama.cpp (our only external native dependency) ---
# Not cloned: the phone builds from the same source tarball as the box, the one on the LocalGhost
# mirror, pinned by SHA-256 in app/src/main/cpp/CMakeLists.txt. The build fetches it from the mirror
# at configure time (or takes a local copy: llamaTarball= in local.properties) and refuses any file
# whose hash differs. Nothing is fetched from GitHub.
REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
CMAKE="$(ls "$REPO_ROOT"/app/src/main/cpp/CMakeLists.txt "$REPO_ROOT"/app/android/app/src/main/cpp/CMakeLists.txt 2>/dev/null | head -1)"
if [ -n "$CMAKE" ]; then
    TB="$(sed -n 's/^set(LLAMA_CPP_TARBALL *"\([^"]*\)".*/\1/p' "$CMAKE")"
    SHA="$(sed -n 's/^set(LLAMA_CPP_SHA256 *"\([^"]*\)".*/\1/p' "$CMAKE")"
    if [ -n "$SHA" ]; then
        echo "> llama.cpp: $TB, SHA-256 $SHA (fetched from the mirror by the first build)"
    else
        echo "> llama.cpp: no pin yet , the APK builds without the phone model (app/android/tools/pin_llama.sh sets it)"
    fi
    # sdk.dir for Gradle, when nothing wrote local.properties yet (release.sh writes its own)
    LP="$(dirname "$(dirname "$(dirname "$(dirname "$(dirname "$CMAKE")")")")")/local.properties"
    if [ ! -f "$LP" ]; then
        echo "sdk.dir=$ANDROID_HOME" > "$LP"
        echo "> wrote $LP (sdk.dir=$ANDROID_HOME)"
    fi
fi

echo ""
echo "Build: cd app/android && ./gradlew assembleDebug   (or installDebug with the phone on USB)"
