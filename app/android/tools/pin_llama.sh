#!/bin/sh
# pin_llama.sh , pin the phone's llama.cpp to the source tarball on the LocalGhost mirror, the same
# bytes the box builds from. Writes LLAMA_CPP_TARBALL / _SHA256 / _TAG / _COMMIT into
# app/src/main/cpp/CMakeLists.txt; the next gradle build compiles the phone model's runtime.
#
#   tools/pin_llama.sh                       read the mirror's signed manifest, pin its llama tarball
#   tools/pin_llama.sh --tarball <file>      pin a copy of the tarball (e.g. from the box's
#                                            /opt/localghost/llama.cpp.mirror-dl/, verified there)
#   tools/pin_llama.sh --key <mirror-key.asc> the site key to check the manifest's signature with
#                                            (default: the server repo's tools/mirror-key.asc)
#   tools/pin_llama.sh --from-box [dir]      on the box: pin exactly the tarball the box's engine was
#                                            built from (dir defaults to /opt/localghost). Its name
#                                            and SHA-256 were recorded when setup_llama.sh verified
#                                            it against the signed manifest; the file is hashed
#                                            again here and must match that record.
#
# The manifest's signature is checked against the site key by fingerprint when gpg and the key are
# at hand, the way the box's tools/mirror_fetch.sh does; without them the script says so and pins
# anyway, and the hash is still checked against the box's own copy if you pass --box-sha.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
CMAKE="$HERE/../app/src/main/cpp/CMakeLists.txt"
MIRROR="${GHOST_MIRROR:-https://www.localghost.ai/mirror}"
MIRROR="${MIRROR%/}"
FPR="${GHOST_MIRROR_FPR:-DCE9A3D14EB461971DD5F393706E4194F08A09A0}"
KEY="${GHOST_MIRROR_KEY:-}"
TARBALL=""; BOX_SHA=""; FROM_BOX=""
while [ $# -gt 0 ]; do
    case "$1" in
        --tarball) TARBALL="$2"; shift 2 ;;
        --key) KEY="$2"; shift 2 ;;
        --mirror) MIRROR="${2%/}"; shift 2 ;;
        --box-sha) BOX_SHA="$2"; shift 2 ;;
        --from-box)
            FROM_BOX="/opt/localghost"; shift
            case "${1:-}" in /*) FROM_BOX="${1%/}"; shift ;; esac ;;
        -h|--help) sed -n '2,21p' "$0"; exit 0 ;;
        *) echo "unknown option $1" >&2; exit 2 ;;
    esac
done
[ -f "$CMAKE" ] || { echo "!! no $CMAKE" >&2; exit 2; }

if [ -n "$FROM_BOX" ]; then
    # the box's own record, written by setup_llama.sh after the manifest check: the name the engine
    # was built from and that file's SHA-256. The tarball it kept must still hash to that.
    SRC="$(cat "$FROM_BOX/llama.cpp/.mirror-src" 2>/dev/null || true)"
    REC="$(cat "$FROM_BOX/llama.cpp/.mirror-sha256" 2>/dev/null || true)"
    [ -n "$SRC" ] && [ -n "$REC" ] || { echo "!! $FROM_BOX/llama.cpp has no .mirror-src/.mirror-sha256 , this box's engine was not built from the mirror (sudo ./tools/update.sh engine)" >&2; exit 3; }
    TARBALL="$FROM_BOX/llama.cpp.mirror-dl/$SRC"
    [ -f "$TARBALL" ] || { echo "!! the box recorded $SRC but $TARBALL is gone" >&2; exit 3; }
    BOX_SHA="$REC"
    echo "-- the box's engine: $SRC, recorded sha256 $REC"
fi

if [ -n "$TARBALL" ]; then
    [ -f "$TARBALL" ] || { echo "!! no such file: $TARBALL" >&2; exit 2; }
    NAME="$(basename "$TARBALL")"
    SHA="$(sha256sum "$TARBALL" | cut -d' ' -f1)"
    echo "-- $NAME: sha256 $SHA (the file you gave)"
else
    T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
    curl -fsSL --proto-redir =https -o "$T/MANIFEST.txt" "$MIRROR/MANIFEST.txt" || { echo "!! could not read $MIRROR/MANIFEST.txt" >&2; exit 3; }
    [ "$(head -1 "$T/MANIFEST.txt")" = "# LocalGhost Mirror Manifest" ] || { echo "!! $MIRROR/MANIFEST.txt is not a mirror manifest" >&2; exit 3; }
    if [ -z "$KEY" ]; then
        for k in "$HERE/../../../server/tools/mirror-key.asc" "$HERE/../../server/tools/mirror-key.asc" "$HOME/localghost/server/tools/mirror-key.asc"; do
            [ -f "$k" ] && { KEY="$k"; break; }
        done
    fi
    if command -v gpg >/dev/null 2>&1 && [ -n "$KEY" ] && [ -f "$KEY" ]; then
        curl -fsSL --proto-redir =https -o "$T/MANIFEST.txt.asc" "$MIRROR/MANIFEST.txt.asc" || { echo "!! no signature at $MIRROR/MANIFEST.txt.asc" >&2; exit 3; }
        mkdir -p "$T/gpg" && chmod 700 "$T/gpg"
        gpg --homedir "$T/gpg" --batch --quiet --import "$KEY" 2>/dev/null
        gpg --homedir "$T/gpg" --batch --status-fd 1 --verify "$T/MANIFEST.txt.asc" "$T/MANIFEST.txt" 2>/dev/null > "$T/status" || true
        if awk '$2 == "VALIDSIG" { print $3; print $NF }' "$T/status" | grep -qx "$FPR"; then
            echo "-- manifest signature: good, the site key $FPR"
        else
            echo "!! the manifest's signature does not verify against $FPR , nothing pinned" >&2; exit 4
        fi
    else
        echo "   (no gpg or no mirror-key.asc here: the manifest's signature was NOT checked; pass --key, or compare with the box: --box-sha)"
    fi
    LINE="$(grep -E '^[0-9a-f]{64}  /[^/]+/llama/llama\.cpp-[A-Za-z0-9._+-]+\.tar\.gz$' "$T/MANIFEST.txt" | tail -1)"
    [ -n "$LINE" ] || { echo "!! the manifest has no llama.cpp tarball in set llama" >&2; exit 3; }
    SHA="${LINE%%  *}"
    NAME="$(basename "${LINE#*  }")"
    echo "-- $NAME: sha256 $SHA (build $(sed -n 's/^# Build: //p' "$T/MANIFEST.txt" | head -1))"
fi
if [ -n "$BOX_SHA" ] && [ "$BOX_SHA" != "$SHA" ]; then
    if [ -n "$FROM_BOX" ]; then
        echo "!! $TARBALL hashes to $SHA, not the $BOX_SHA the box recorded when it verified it; nothing pinned" >&2
    else
        echo "!! the box's copy has sha256 $BOX_SHA , not the same tarball; nothing pinned" >&2
    fi
    exit 4
fi

# llama.cpp-<tag>-<commit>.tar.gz
BASE="${NAME%.tar.gz}"; BASE="${BASE#llama.cpp-}"
TAG="${BASE%-*}"; COMMIT="${BASE##*-}"
[ "$TAG" = "$BASE" ] && { TAG="$BASE"; COMMIT=""; }

set_var() { # set_var NAME VALUE , rewrite `set(NAME "…"` in CMakeLists.txt
    sed -i "s|^set($1 \\{1,\\}\"[^\"]*\"|set($1 $(printf '%*s' $((18 - ${#1})) '')\"$2\"|" "$CMAKE"
    grep -q "^set($1 .*\"$2\"" "$CMAKE" || { echo "!! could not write $1 into $CMAKE" >&2; exit 5; }
}
set_var LLAMA_CPP_TARBALL "$NAME"
set_var LLAMA_CPP_SHA256 "$SHA"
set_var LLAMA_CPP_TAG "$TAG"
set_var LLAMA_CPP_COMMIT "$COMMIT"
echo "== pinned: $NAME ($TAG, $COMMIT), sha256 $SHA"
echo "   the next ./gradlew build compiles the phone model's runtime from it (-PllamaTarball=<file> to build from a local copy)"
