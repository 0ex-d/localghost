#!/usr/bin/env bash
# setup_llama.sh , the from-NOTHING inference setup. Run as root, before first unlock.
#
# Assumes a bare box: no llama.cpp, no binary, no model. It:
#   1. installs build deps (git, cmake, compiler)
#   2. clones llama.cpp into ITS OWN FOLDER (/opt/localghost/llama.cpp) and builds llama-server there
#   3. symlinks the built binary to /usr/local/bin/llama-server (oracled's default llamaBin)
#   4. downloads the model ggufs if URLs are provided, or takes a local dir of ggufs
#   5. hands everything to stage_models.sh , the next unlock ingests onto the encrypted volume
#
# Usage:
#   sudo ./tools/setup_llama.sh --models /path/to/dir-with-ggufs
#   sudo ./tools/setup_llama.sh --model-url URL [--mmproj-url URL] [--embed-url URL] [--hf-token TOKEN]
#   sudo ./tools/setup_llama.sh --build-only
#
# HONEST NOTE on the model download: Gemma weights on Hugging Face are LICENSE-GATED , a fully
# unattended download needs an HF token (--hf-token or HF_TOKEN env) from an account that accepted
# the license. Without one, download the ggufs on any machine, copy them over, and use --models.
set -eu

if [ "$(id -u)" -ne 0 ]; then echo "run as root" >&2; exit 1; fi
cd "$(dirname "$0")/.."   # repo root, so ./tools/stage_models.sh resolves

LLAMA_DIR=/opt/localghost/llama.cpp
MODELS_DIR=""
MODEL_URL=""
MMPROJ_URL=""
EMBED_URL=""
HF_TOKEN="${HF_TOKEN:-}"
BUILD_ONLY=0
while [ $# -gt 0 ]; do
    case "$1" in
        --models)     MODELS_DIR="$2"; shift 2 ;;
        --model-url)  MODEL_URL="$2"; shift 2 ;;
        --mmproj-url) MMPROJ_URL="$2"; shift 2 ;;
        --embed-url)  EMBED_URL="$2"; shift 2 ;;
        --hf-token)   HF_TOKEN="$2"; shift 2 ;;
        --build-only) BUILD_ONLY=1; shift ;;
        *) echo "unknown arg: $1" >&2; exit 2 ;;
    esac
done

echo "=== 1/4  build dependencies ==="
apt-get install -y --no-install-recommends git cmake build-essential ca-certificates curl libcurl4-openssl-dev

echo "=== 2/4  llama.cpp , clone + build in its own folder ==="
if [ ! -d "$LLAMA_DIR/.git" ]; then
    git clone --depth 1 https://github.com/ggml-org/llama.cpp "$LLAMA_DIR"
else
    git -C "$LLAMA_DIR" pull --ff-only || echo "-- pull failed (offline?), building what is checked out"
fi
if [ ! -x "$LLAMA_DIR/build/bin/llama-server" ]; then
    # CPU build , this box has no GPU; -DGGML_NATIVE=ON tunes for THIS machine's instruction set.
    cmake -S "$LLAMA_DIR" -B "$LLAMA_DIR/build" -DGGML_NATIVE=ON -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_SERVER=ON
    cmake --build "$LLAMA_DIR/build" --target llama-server -j"$(nproc)"
else
    echo "-- llama-server already built, skipping (delete $LLAMA_DIR/build to force rebuild)"
fi
ln -sf "$LLAMA_DIR/build/bin/llama-server" /usr/local/bin/llama-server
echo "-- $(/usr/local/bin/llama-server --version 2>&1 | head -1 || echo 'llama-server linked')"

if [ "$BUILD_ONLY" -eq 1 ]; then
    echo "build-only requested , done. Stage models later with tools/stage_models.sh"
    exit 0
fi

echo "=== 3/4  model weights ==="
DL=/var/lib/ghost/staging/download
if [ -z "$MODELS_DIR" ]; then
    if [ -z "$MODEL_URL" ]; then
        echo "!! no --models dir and no --model-url. Download the ggufs elsewhere and re-run with --models." >&2
        exit 3
    fi
    mkdir -p "$DL"; chmod 700 "$DL"
    AUTH=()
    [ -n "$HF_TOKEN" ] && AUTH=(-H "Authorization: Bearer $HF_TOKEN")
    fetch() { # url -> file in $DL, resumable, fail loudly with the URL named
        local url="$1" out="$DL/$(basename "${1%%\?*}")"
        echo "-- fetching $(basename "$out")"
        if ! curl -fL --retry 3 -C - "${AUTH[@]}" -o "$out" "$url"; then
            echo "!! download failed: $url" >&2
            echo "   (gated model? pass --hf-token, or download manually and use --models)" >&2
            exit 4
        fi
    }
    fetch "$MODEL_URL"
    [ -n "$MMPROJ_URL" ] && fetch "$MMPROJ_URL"
    [ -n "$EMBED_URL" ] && fetch "$EMBED_URL"
    MODELS_DIR="$DL"
fi

echo "=== 4/4  stage for ingest at next unlock ==="
./tools/stage_models.sh "$MODELS_DIR"
# staged copies live under /var/lib/ghost/staging/ai-models; remove the download scratch if we made it
if [ -d "$DL" ] && [ "$MODELS_DIR" = "$DL" ]; then rm -rf "$DL"; fi

echo "----------------------------------------"
echo "Inference setup complete:"
echo "  llama.cpp    $LLAMA_DIR (self-contained; binary symlinked to /usr/local/bin/llama-server)"
echo "  models       staged , the NEXT UNLOCK ingests them onto the encrypted volume"
echo "Unlock from the app, then verify:"
echo "  sudo journalctl -u ghost.secd --since '2 min ago' | grep -i 'ingested\\|oracled'"
echo "  sudo ./tools/ns.sh ./bin/ghost-cli ghost.oracled status"
