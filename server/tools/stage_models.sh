#!/usr/bin/env bash
# stage_models.sh , provision-time model staging. Run as root, BEFORE first unlock.
#
# The encrypted volume does not exist until the first unlock, so models cannot be placed directly.
# This script stages them on the unencrypted disk at /var/lib/ghost/staging/ai-models (root-only),
# and ghost.secd INGESTS them onto the encrypted volume automatically during unlock , move, chown to
# the run user, remove the staged copy. From-scratch flow becomes: provision, stage, unlock, done.
#
#   sudo ./tools/stage_models.sh /path/to/dir-with-ggufs [/path/to/llama-server]
#
# The source dir should contain the gguf files (main model, mmproj, embedding model , whatever the
# conf expects; defaults are gemma-4-12b-it-Q4_K_M.gguf, mmproj-F16.gguf, embeddinggemma-300m-q8.gguf).
# The optional second argument installs the llama-server binary to /usr/local/bin (it is not secret).
#
# NOTE ON PLAINTEXT: staged files sit on the UNENCRYPTED disk until the next unlock ingests them.
# Stage right before unlocking, and if the source copies elsewhere on this disk are no longer needed,
# shred them , this script deliberately does not delete your sources.
set -eu

if [ "$(id -u)" -ne 0 ]; then echo "run as root" >&2; exit 1; fi
SRC="${1:-}"
LLAMA_BIN="${2:-}"
if [ -z "$SRC" ] || [ ! -d "$SRC" ]; then
    echo "usage: $0 /path/to/dir-with-ggufs [/path/to/llama-server]" >&2
    exit 2
fi

STAGING=/var/lib/ghost/staging/ai-models
mkdir -p "$STAGING"
chmod 700 /var/lib/ghost/staging "$STAGING"

# 1. Stop anything already serving a model , a leftover llama-server holds the RAM the cohort needs,
#    and two copies of 12B weights in memory ends with the OOM killer choosing for you.
FOUND="$(pgrep -af 'llama-server|llama\.cpp' | grep -v "$0" || true)"
if [ -n "$FOUND" ]; then
    echo "-- stopping existing llama processes:"
    echo "$FOUND"
    pgrep -f 'llama-server|llama\.cpp' | while read -r pid; do
        kill -TERM "$pid" 2>/dev/null || true
    done
    sleep 2
fi
# disable any unit that would bring one back on boot
for unit in $(systemctl list-unit-files --type=service --no-legend 2>/dev/null | awk '{print $1}' | grep -i llama || true); do
    echo "-- disabling unit $unit"
    systemctl stop "$unit" 2>/dev/null || true
    systemctl disable "$unit" 2>/dev/null || true
done

# 2. Install the binary (not secret , lives on the OS disk like any tool).
if [ -n "$LLAMA_BIN" ]; then
    install -m 0755 "$LLAMA_BIN" /usr/local/bin/llama-server
    echo "-- llama-server installed to /usr/local/bin"
elif ! command -v llama-server >/dev/null 2>&1; then
    echo "!! no llama-server on PATH and none provided , oracled will name this at unlock" >&2
fi

# 3. Stage the weights.
COUNT=0
for f in "$SRC"/*.gguf; do
    [ -e "$f" ] || continue
    SIZE=$(stat -c%s "$f")
    if [ "$SIZE" -lt 1048576 ]; then
        echo "!! skipping $(basename "$f") , ${SIZE} bytes is not a model (interrupted download?)" >&2
        continue
    fi
    echo "-- staging $(basename "$f") ($((SIZE / 1048576))MB)"
    cp "$f" "$STAGING/"
    COUNT=$((COUNT + 1))
done
chmod 600 "$STAGING"/*.gguf 2>/dev/null || true

if [ "$COUNT" -eq 0 ]; then
    echo "!! no gguf files staged from $SRC" >&2
    exit 3
fi
echo "----------------------------------------"
echo "$COUNT model file(s) staged at $STAGING (root-only, unencrypted until ingest)."
echo "Next unlock moves them onto the encrypted volume automatically and removes the staged copies."
echo "If your source copies at $SRC are on this disk and no longer needed:  shred -u $SRC/*.gguf"
