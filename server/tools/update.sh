#!/usr/bin/env bash
# update.sh , bring a RUNNING, UNLOCKED box current with the LocalGhost mirror, in one command.
#
# redeploy.sh ships CODE (build, stage, restart) and reaches no network. This ships DATA: every set
# the mirror carries that this box uses, each checked the way setup checks it (the manifest's
# signature against the site key, every file's SHA-256), each fetched only when the mirror has
# bytes this box does not. A rerun when everything is current costs a few manifest reads.
#
#   sudo ./tools/update.sh                     everything below, in this order
#   sudo ./tools/update.sh maps embedder       only these steps
#   sudo GHOST_GEO_ROADS=europe-latest.osm.pbf ./tools/update.sh maps    add a continent's streets
#
#   maps      GeoNames + Natural Earth (geo), the OpenStreetMap coastline (landpolygons), and the
#             roads extracts this box already has (roads; GHOST_GEO_ROADS adds more). Straight onto
#             the volume; ghost.framed then imports the names and cuts the tiles in the background.
#   embedder  EmbeddingGemma 300M QAT (embeddings) into the volume's ai-models; ghost.searchd is
#             restarted onto it and embeds the archive again by itself (vectors never mix models).
#   weights   the chat/vision model files checked against tools/model.pins; any that do not match
#             are replaced from the mirror and ghost.oracled restarted (tools/models_check.sh --fix).
#   phone     the model the box offers its phones (tools/phone_model.sh, the system area).
#   engine    llama.cpp from the mirror's pinned tarball (tools/setup_llama.sh --build-only): built
#             again only when the tarball changed (or this box still has a git checkout), then put
#             on the volume and ghost.oracled restarted onto it.
#
# The mirror only, as at setup: a set the mirror does not list yet is said and skipped; the mirror
# not answering stops the run before anything is touched. What this reveals is what setup reveals:
# that this IP fetched public files. Nothing of the box's own goes out, and nothing runs by itself:
# the operator runs this, when they choose to.
set -uo pipefail

if [ "$(id -u)" -ne 0 ]; then echo "run as root (sudo)" >&2; exit 1; fi
REPO="$(cd "$(dirname "$0")/.." && pwd)"
TOOLS="$REPO/tools"
MOUNT="${GHOST_MOUNT:-/var/lib/ghost/mnt/slot0}"
case "$MOUNT" in /proc/*) echo "run update.sh itself, not through ns.sh (it enters the volume where it needs to)" >&2; exit 2 ;; esac
PID="$(pidof ghost.secd || true)"; PID="${PID%% *}"
if [ -z "$PID" ]; then echo "ghost.secd is not running" >&2; exit 1; fi
DOOR="/proc/$PID/root"
if [ ! -d "$DOOR$MOUNT/run" ]; then
    echo "the box is LOCKED: unlock it from the app first (the maps and the weights live on the encrypted volume)" >&2
    exit 1
fi
OWNER="$(stat -c %U "$DOOR$MOUNT/run" 2>/dev/null || echo coder)"
CLI="$REPO/bin/ghost-cli"; [ -x "$CLI" ] || CLI=/opt/localghost/bin/ghost-cli
CTL="$REPO/bin/ghost-ctl"; [ -x "$CTL" ] || CTL=/opt/localghost/bin/ghost-ctl

STEPS="${*:-maps embedder weights phone engine}"
for s in $STEPS; do
    case "$s" in maps|embedder|weights|phone|engine) ;; *) echo "unknown step '$s' (maps embedder weights phone engine)" >&2; exit 2 ;; esac
done

# has_cuda <binary>: linked against CUDA, or CUDA compiled in (the check tools/gpu.sh makes)
has_cuda() {
    [ -x "$1" ] || return 1
    ldd "$1" 2>/dev/null | grep -qEi 'cuda|cublas' && return 0
    strings -n 8 "$1" 2>/dev/null | grep -q 'ggml_cuda_init'
}
_t0=$(date +%s)
say() { printf '\n=== %s ===  (+%ss)\n' "$1" "$(( $(date +%s) - _t0 ))"; }
RESULTS=()
result() { RESULTS+=("$(printf '%-9s %s' "$1" "$2")"); }
failed=0

# THE MIRROR FIRST: unreachable means stop, before anything is half-done
say "the mirror"
command -v gpg >/dev/null 2>&1 || apt-get install -y gpg >/dev/null 2>&1 || true
mrc=0; sh "$TOOLS/mirror_fetch.sh" --list geo >/dev/null || mrc=$?
if [ "$mrc" = 1 ]; then
    echo "the mirror could not be read (above) , nothing updated. Try again when it answers."
    exit 1
fi
echo "the mirror answers; its signature checks"

for step in $STEPS; do
case "$step" in
maps)
    say "maps: place names, the coastline, the roads"
    GEO="$MOUNT/geo"
    # through ns.sh's host-side door: the script runs from the repo, its paths land on the volume;
    # the cutting is left to ghost.framed, which does it in the background and serves the old
    # tiles until the new ones are whole
    GHOST_GEO_NO_CUT=1 "$TOOLS/ns.sh" "$TOOLS/fetch_geo.sh" "$GEO"
    chown -R "$OWNER:$OWNER" "$DOOR$GEO" 2>/dev/null || true
    ch="$(sort -u "$DOOR$GEO/.fetch-geo-changed" 2>/dev/null | tr '\n' ' ')"
    rm -f "$DOOR$GEO/.fetch-geo-changed"
    asked=""
    case " $ch " in *" geo "*)
        "$TOOLS/ns.sh" "$CLI" ghost.framed geo-import >/dev/null && asked="$asked, place names importing" ;;
    esac
    case " $ch " in *" landtiles "*)
        "$TOOLS/ns.sh" "$CLI" ghost.framed geo-tiles >/dev/null && asked="$asked, coastline cutting" ;;
    esac
    case " $ch " in *" roadtiles "*)
        "$TOOLS/ns.sh" "$CLI" ghost.framed road-tiles >/dev/null && asked="$asked, streets cutting (hours for a continent)" ;;
    esac
    got="$(printf '%s\n' $ch | grep -xE 'geo|landpolygons|roads' | tr '\n' ' ')"
    if [ -n "$got" ]; then result maps "updated: ${got% }$asked , ghost.framed's log follows it"
    elif [ -n "$asked" ]; then result maps "current${asked}"
    else result maps "current (a set not on the mirror is named above)"; fi
    ;;
embedder)
    say "embedder: EmbeddingGemma 300M QAT"
    AI="$DOOR$MOUNT/ai-models"; E=embeddinggemma-300m-qat-Q8_0.gguf
    before="$(cat "$AI/.$E.sha256" 2>/dev/null)"
    erc=0; sh "$TOOLS/mirror_fetch.sh" embeddings "$AI" "$E" || erc=$?
    [ -f "$AI/NOTICE.txt" ] && mv -f "$AI/NOTICE.txt" "$AI/NOTICE-embeddings.txt"
    rm -f "$AI/.mirror-files"
    if [ "$erc" = 0 ] && [ -s "$AI/$E" ]; then
        chown "$OWNER:$OWNER" "$AI/$E" "$AI"/NOTICE-embeddings.txt "$AI"/TERMS-*.txt 2>/dev/null || true
        chmod 600 "$AI/$E"
        if [ "$(cat "$AI/.$E.sha256" 2>/dev/null)" != "$before" ]; then
            # searchd picks the QAT build at start and queues the archive to be embedded again
            if "$CTL" restart-daemon ghost.searchd >/dev/null 2>&1; then
                result embedder "installed; ghost.searchd restarted onto it and re-embeds the archive in the background"
            else
                result embedder "installed; restart ghost.searchd to use it: sudo $CTL restart-daemon ghost.searchd"
            fi
        else
            result embedder "current"
        fi
    elif [ "$erc" = 3 ]; then result embedder "not on the mirror yet (set embeddings)"
    else result embedder "FAILED (above; a rerun resumes)"; failed=1; fi
    ;;
weights)
    say "weights: the model files against tools/model.pins"
    wrc=0; "$TOOLS/ns.sh" "$TOOLS/models_check.sh" --fix || wrc=$?
    case "$wrc" in
        0) result weights "the pinned files (replaced from the mirror where they were not; see above)" ;;
        *) result weights "FAILED (above)"; failed=1 ;;
    esac
    ;;
phone)
    say "phone: the model this box offers its phones"
    prc=0; sh "$TOOLS/phone_model.sh" || prc=$?
    case "$prc" in
        0) result phone "offered to phones (MODELS in the app's menu)" ;;
        3) result phone "not on the mirror yet (set phone); phones read with the box's model meanwhile" ;;
        *) result phone "FAILED (above)"; failed=1 ;;
    esac
    ;;
engine)
    say "engine: llama.cpp from the mirror's pinned source"
    brc=0; bash "$TOOLS/setup_llama.sh" --build-only || brc=$?
    NEW="$REPO/bin/llama-server"; ON="$DOOR$MOUNT/bin/llama-server"
    if [ "$brc" != 0 ] || [ ! -x "$NEW" ]; then
        result engine "FAILED (above); the llama-server on the volume keeps running"; failed=1
    elif cmp -s "$NEW" "$ON"; then
        result engine "current: $(cat /opt/localghost/llama.cpp/.mirror-src 2>/dev/null || echo '?')"
    elif has_cuda "$ON" && ! has_cuda "$NEW"; then
        # a CPU-only build over the GPU one would put the 12B at a few tokens a second
        result engine "NOT swapped: the new build has no CUDA (nvcc not found?) and the volume's does; see setup_llama's output"
        failed=1
    else
        # onto the volume beside the old one, then renamed over it (a running binary is never
        # rewritten in place); oracled's restart starts the new llama-server
        install -m755 "$NEW" "$ON.new" && chown "$OWNER:$OWNER" "$ON.new" && mv -f "$ON.new" "$ON"
        if "$CTL" restart-daemon ghost.oracled >/dev/null 2>&1; then
            result engine "$(cat /opt/localghost/llama.cpp/.mirror-src 2>/dev/null || echo 'the new build') on the volume; ghost.oracled restarted onto it"
        else
            result engine "on the volume; restart ghost.oracled to use it: sudo $CTL restart-daemon ghost.oracled"
        fi
    fi
    ;;
esac
done

printf '\n----------------------------------------\nmirror update, %ss:\n' "$(( $(date +%s) - _t0 ))"
for r in "${RESULTS[@]}"; do printf '  %s\n' "$r"; done
echo "then: sudo ./tools/health.sh"
exit "$failed"
