#!/bin/sh
# phone_model.sh , put the phone's model where secd offers it to phones.
#
#   sudo ./tools/phone_model.sh                 fetch the pinned model from the mirror, check it,
#                                               install it, write the catalogue (setup_llama.sh runs this)
#   sudo ./tools/phone_model.sh --file <gguf>   the same from a file you copied over (checked against the pin)
#   sudo ./tools/phone_model.sh --check         what the catalogue offers, and whether each file matches it
#
# The phone never downloads from the internet: it pulls the model from its own box over the same
# mutually-authenticated link as everything else (GET /v1/models, /v1/models/<id>, resumable), and
# checks the SHA-256 the catalogue states. The box gets it once, at setup, here: from the LocalGhost
# mirror's signed `phone` set and checked against tools/phone_model.pins as well. Not on the mirror
# yet, or the mirror unreachable: said, and nothing installed (GHOST_MIRROR_UPSTREAM=1 takes the
# pin's upstream, Hugging Face without an account, on the operator's own authority, checked by the
# pin alone). The directory is the box's unencrypted system area
# (<state dir>/models, default /var/lib/ghost/models): a phone model is not anyone's data, and
# secd serves it without an unlocked volume.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
GHOST_MODEL_PINS="${GHOST_PHONE_MODEL_PINS:-$HERE/phone_model.pins}"
export GHOST_MODEL_PINS
. "$HERE/model_pins.sh"
STATE="${GHOST_STATE_DIR:-/var/lib/ghost}"
DIR="${GHOST_PHONE_MODELS_DIR:-$STATE/models}"
FETCH="$HERE/mirror_fetch.sh"
SVC_USER="${GHOST_SERVICE_USER:-coder}"
NAME="gemma-4-E2B-it-qat-UD-Q2_K_XL.gguf"
ID="gemma-4-e2b-qat"
TITLE="Gemma 4 E2B (QAT, 2-bit)"
DETAIL="reads web pages into notes when the box is slow; answers by itself when there is no box. Apache 2.0"

FILE=""; CHECK=0
while [ $# -gt 0 ]; do
    case "$1" in
        --file) FILE="$2"; shift 2 ;;
        --check) CHECK=1; shift ;;
        -h|--help) sed -n '2,16p' "$0"; exit 0 ;;
        *) echo "unknown option $1" >&2; exit 2 ;;
    esac
done

if [ "$CHECK" = 1 ]; then
    echo "catalogue: $DIR/catalog.json"
    [ -f "$DIR/catalog.json" ] || { echo "   none , the box offers phones no model yet (sudo ./tools/phone_model.sh)"; exit 1; }
    cat "$DIR/catalog.json"; echo
    for f in "$DIR"/*.gguf; do
        [ -e "$f" ] || continue
        pin_check "$f" || true
    done
    exit 0
fi

[ "$(id -u)" = 0 ] || { echo "run as root (sudo): $DIR is the box's system area" >&2; exit 1; }
mkdir -p "$DIR"
DL="$DIR/.dl"; mkdir -p "$DL"
OUT="$DL/$NAME"

if [ -n "$FILE" ]; then
    [ -f "$FILE" ] || { echo "!! no such file: $FILE" >&2; exit 2; }
    echo "-- $NAME from $FILE"
    cp "$FILE" "$OUT.part" && mv -f "$OUT.part" "$OUT"
elif [ -s "$DIR/$NAME" ] && pin_check "$DIR/$NAME" >/dev/null 2>&1; then
    echo "-- $NAME already installed and matches the pin"
    OUT="$DIR/$NAME"
elif [ -s "$OUT" ] && pin_check "$OUT" >/dev/null 2>&1; then
    echo "-- $NAME already downloaded and matches the pin"
else
    rc=0; sh "$FETCH" phone "$DL" "$NAME" || rc=$?
    if [ "$rc" = 0 ] && [ -s "$OUT" ]; then
        echo "-- $NAME from the mirror (signed manifest checked)"
    elif [ "${GHOST_MIRROR_UPSTREAM:-}" = 1 ]; then
        URL="$(pin_url "$NAME")"
        echo "-- !! GHOST_MIRROR_UPSTREAM=1: fetching $NAME from $URL (2.2 GB, resumable) , checked by"
        echo "      tools/phone_model.pins only, NOT a signed manifest"
        curl -fL --proto-redir =https --retry 3 --retry-delay 5 -C - --progress-bar -o "$OUT" "$URL" || { echo "!! download failed: $URL" >&2; exit 3; }
    else
        if [ "$rc" = 3 ]; then
            echo "!! $NAME is not on the mirror yet (set phone; not published in this build) , not installed" >&2
        else
            echo "!! could not take $NAME from the mirror (the lines above say why; a rerun resumes) , not installed" >&2
        fi
        echo "   phones read with the box's model meanwhile. Re-run when the mirror has it, or --file <copy>." >&2
        rmdir "$DL" "$DIR" 2>/dev/null || true   # only when nothing is there (a half download stays, to resume)
        exit "$rc"
    fi
fi
pin_check "$OUT" || { echo "!! $NAME does not match tools/phone_model.pins , not installed" >&2; rm -f "$OUT"; exit 4; }
if [ "$OUT" != "$DIR/$NAME" ]; then
    mv -f "$OUT" "$DIR/$NAME"
fi
chmod 0644 "$DIR/$NAME"
cp "$DL"/NOTICE.txt "$DL"/TERMS-*.txt "$DIR/" 2>/dev/null || true

# the catalogue secd serves (internal/models: id, name, detail, sizeBytes, sha256, file)
SHA="$(pin_sha "$NAME")"
SIZE="$(stat -c%s "$DIR/$NAME")"
cat > "$DIR/catalog.json.tmp" <<JSON
[
  {"id": "$ID", "name": "$TITLE", "detail": "$DETAIL", "sizeBytes": $SIZE, "sha256": "$SHA", "file": "$NAME"}
]
JSON
mv -f "$DIR/catalog.json.tmp" "$DIR/catalog.json"
chmod 0644 "$DIR/catalog.json"
chown -R "$SVC_USER" "$DIR" 2>/dev/null || true
rm -rf "$DL"
echo "== the box offers phones: $TITLE ($SIZE bytes) , the app downloads it from MODELS in the menu"
