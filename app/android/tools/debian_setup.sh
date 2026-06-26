#!/usr/bin/env bash
# One-time setup on Debian 13 to build the LocalGhost APK headlessly (no Android Studio).
# Installs JDK 17, the Android command-line tools, and the exact SDK packages this project uses.
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
sdkmanager --sdk_root="$ANDROID_HOME" \
    "platform-tools" \
    "platforms;android-37.0" \
    "platforms;android-36" \
    "build-tools;36.0.0"

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
# Pinned by IMMUTABLE COMMIT (not just the tag) and verified after fetch. The pin lives only in
# CMakeLists.txt (LLAMA_CPP_TAG + LLAMA_CPP_COMMIT). This step pre-clones at that commit so the
# native build runs offline and the exact source is part of the deploy, verifies it, resolves the
# full SHA for the tag if the pin still has the placeholder, and checks GitHub for a newer release.
REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo .)"
CMAKE="$REPO_ROOT/app/src/main/cpp/CMakeLists.txt"
LLAMA_REPO="https://github.com/ggml-org/llama.cpp"
LLAMA_DIR="$REPO_ROOT/.cache/llama.cpp"
LLAMA_TAG="$(grep -oE 'LLAMA_CPP_TAG[^"]*"[^"]+"' "$CMAKE" 2>/dev/null | grep -oE '"[^"]+"$' | tr -d '"')"
LLAMA_COMMIT="$(grep -oE 'LLAMA_CPP_COMMIT[^"]*"[^"]+"' "$CMAKE" 2>/dev/null | grep -oE '"[^"]+"$' | tr -d '"')"

if [ -n "$LLAMA_TAG" ]; then
    echo "> llama.cpp pinned at tag $LLAMA_TAG, commit ${LLAMA_COMMIT:0:12} ..."

    # Resolve the full 40-char SHA the tag points to (so we can verify and, if needed, fill the pin).
    RESOLVED="$(git ls-remote "$LLAMA_REPO" "refs/tags/$LLAMA_TAG^{}" 2>/dev/null | awk '{print $1}' | head -1)"
    [ -z "$RESOLVED" ] && RESOLVED="$(git ls-remote "$LLAMA_REPO" "refs/tags/$LLAMA_TAG" 2>/dev/null | awk '{print $1}' | head -1)"

    if echo "$LLAMA_COMMIT" | grep -q "REPLACE_WITH_FULL"; then
        if [ -n "$RESOLVED" ]; then
            echo "  [action needed] LLAMA_CPP_COMMIT is a placeholder. Tag $LLAMA_TAG resolves to:"
            echo "      $RESOLVED"
            echo "  Set LLAMA_CPP_COMMIT = \"$RESOLVED\" in $CMAKE, then re-run, so the build verifies it."
        else
            echo "  [!] could not resolve $LLAMA_TAG from GitHub to fill the commit pin (offline?)."
        fi
    elif [ -n "$RESOLVED" ] && [ "$RESOLVED" != "$LLAMA_COMMIT" ]; then
        echo "  [!] WARNING: pinned commit does not match what tag $LLAMA_TAG resolves to now."
        echo "      pinned:   $LLAMA_COMMIT"
        echo "      tag now:  $RESOLVED"
        echo "      The tag may have been re-pointed. NOT changing the pin; investigate before bumping."
    fi

    # Fetch the exact pinned commit (skip if still placeholder).
    if ! echo "$LLAMA_COMMIT" | grep -q "REPLACE_WITH_FULL"; then
        mkdir -p "$LLAMA_DIR"
        if [ ! -d "$LLAMA_DIR/.git" ]; then
            ( cd "$LLAMA_DIR" && git init -q && git remote add origin "$LLAMA_REPO.git" )
        fi
        ( cd "$LLAMA_DIR" \
            && git fetch -q --depth 1 origin "$LLAMA_COMMIT" \
            && git checkout -q "$LLAMA_COMMIT" ) \
            || echo "  fetch of $LLAMA_COMMIT failed (check the pin / connectivity)"
        # Verify what we checked out.
        if [ -d "$LLAMA_DIR/.git" ]; then
            GOT="$(cd "$LLAMA_DIR" && git rev-parse HEAD 2>/dev/null)"
            if [ "$GOT" = "$LLAMA_COMMIT" ]; then
                echo "  verified: llama.cpp at $LLAMA_COMMIT"
            else
                echo "  [!] MISMATCH: got $GOT, expected $LLAMA_COMMIT. Do not build."
            fi
        fi
    fi

    # Inform about a newer release (does NOT bump).
    LATEST="$(curl -fsSL https://api.github.com/repos/ggml-org/llama.cpp/releases/latest 2>/dev/null \
        | grep -oE '"tag_name"[^,]*' | grep -oE 'b[0-9]+' | head -1)"
    STAMP="$REPO_ROOT/.cache/llama-version-check.txt"; mkdir -p "$REPO_ROOT/.cache"
    if [ -n "$LATEST" ]; then
        echo "pinned_tag=$LLAMA_TAG pinned_commit=$LLAMA_COMMIT latest_tag=$LATEST checked=$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$STAMP"
        if [ "$LATEST" != "$LLAMA_TAG" ]; then
            echo "  [i] newer llama.cpp available: $LATEST (pinned: $LLAMA_TAG). To adopt, update both"
            echo "      LLAMA_CPP_TAG and LLAMA_CPP_COMMIT in $CMAKE, rebuild, re-test the JNI."
        else
            echo "  up to date with upstream latest ($LATEST)."
        fi
    else
        echo "  (could not reach GitHub for a version check; offline is fine)"
    fi
else
    echo "> (could not read LLAMA_CPP_TAG from $CMAKE; skipping llama.cpp pre-fetch)"
fi

echo ""
echo "The build also needs a local.properties pointing at the SDK. The release script writes it."
