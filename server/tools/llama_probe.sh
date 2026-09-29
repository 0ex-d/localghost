#!/usr/bin/env bash
# llama_probe.sh , run the volume's llama-server by hand, the way oracled starts it, on a spare
# loopback port, and send it what the box sends: one text question, then two photos the way a
# caption does (text part first, then the image as a data URI). Prints what it says about the GPU,
# what it answers, and , if it dies , how it died and its last lines.
#
# Written 29 Sep 2026: llama-server (mirror build v0.5.0) died on the first caption after every
# start, and its own output goes nowhere (watchd gives daemons no stdout), so the reason was lost.
#
# Changes nothing: its own port (18090), its own log (deleted at the end unless --keep), the child
# killed on exit. oracled's port 18080 and searchd's embedder on 18081 are left alone. Reads the
# unlocked volume through ghost.secd's namespace door, like ns.sh.
#
#   sudo ./tools/llama_probe.sh            # the conf's extraArgs (minus --mlock)
#   sudo ./tools/llama_probe.sh --small    # -c 8192 --parallel 1: is it the context size?
#   sudo ./tools/llama_probe.sh --cpu      # -ngl 0: is it the GPU?
#   add --keep to leave the full log in /tmp
set -u
[ "$(id -u)" -eq 0 ] || { echo "run as root (sudo)" >&2; exit 1; }
SMALL=0; CPU=0; KEEP=0
for a in "$@"; do case "$a" in --small) SMALL=1 ;; --cpu) CPU=1 ;; --keep) KEEP=1 ;; *) echo "unknown: $a" >&2; exit 1 ;; esac; done
PID="$(pidof ghost.secd || true)"; PID="${PID%% *}"
[ -n "$PID" ] || { echo "ghost.secd is not running , nothing to probe" >&2; exit 1; }
R="/proc/$PID/root"
M="/var/lib/ghost/mnt/slot0"
CONF="$R$M/conf/ghost.oracled.conf"
[ -f "$CONF" ] || { echo "no $M/conf/ghost.oracled.conf , is the box unlocked?" >&2; exit 1; }
jget() { tr -d '\n' < "$CONF" | sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p"; }
BIN="$(jget llamaBin)"; BIN="${BIN:-$M/bin/llama-server}"
MODEL="$(jget modelPath)"; MODEL="${MODEL:-$M/ai-models/gemma-4-12b-it-Q4_K_M.gguf}"
MMPROJ="$(jget mmprojPath)"; MMPROJ="${MMPROJ:-$M/ai-models/mmproj-F16.gguf}"
EXTRA=()
while IFS= read -r x || [ -n "$x" ]; do [ -n "$x" ] && [ "$x" != "--mlock" ] && EXTRA+=("$x"); done < <(
    tr -d '\n' < "$CONF" | sed -n 's/.*"extraArgs"[[:space:]]*:[[:space:]]*\[\([^]]*\)\].*/\1/p' |
    tr ',' '\n' | sed 's/^[[:space:]]*"//; s/"[[:space:]]*$//')
[ "$SMALL" = 1 ] && EXTRA+=(-c 8192 --parallel 1)
NGL=99; [ "$CPU" = 1 ] && EXTRA+=(-ngl 0) # last wins in llama's parser, over a conf --n-gpu-layers
PORT=18090
if (exec 3<>/dev/tcp/127.0.0.1/$PORT) 2>/dev/null; then echo "port $PORT is taken , is another probe still running?" >&2; exit 1; fi
LOG="$(mktemp /tmp/llama-probe.XXXXXX.log)"; BODY="$(mktemp /tmp/llama-probe.XXXXXX.json)"; OUT="$(mktemp /tmp/llama-probe.XXXXXX.out)"
LP=""; B64=""
cleanup() {
    [ -n "$LP" ] && kill "$LP" 2>/dev/null && sleep 1 && kill -9 "$LP" 2>/dev/null
    rm -f "$BODY" "$OUT" ${B64:+"$B64"}
    if [ "$KEEP" = 1 ]; then echo "full log kept: $LOG"; else rm -f "$LOG"; fi
}
trap cleanup EXIT

echo "== the engine"
echo "   binary: $BIN ($(stat -c %y "$R$BIN" 2>/dev/null | cut -d. -f1))"
echo "   model:  $MODEL ($(du -h "$R$MODEL" 2>/dev/null | cut -f1))"
echo "   mmproj: $MMPROJ ($(du -h "$R$MMPROJ" 2>/dev/null | cut -f1))"
echo "   args:   -ngl $NGL ${EXTRA[*]}"
echo "   gpu:    $(timeout 15 nvidia-smi --query-gpu=name,memory.used,memory.total --format=csv,noheader 2>&1 | head -1)"
t0=$(date +%s)
"$R$BIN" -m "$R$MODEL" --host 127.0.0.1 --port $PORT --no-webui -ngl $NGL --mmproj "$R$MMPROJ" "${EXTRA[@]}" >"$LOG" 2>&1 &
LP=$!

alive() { kill -0 "$LP" 2>/dev/null; }
died() {
    wait "$LP" 2>/dev/null; rc=$?
    LP=""
    echo
    echo "!! llama-server DIED (exit $rc$( [ $rc -gt 128 ] && echo ", signal $((rc-128)): $(kill -l $((rc-128)) 2>/dev/null)"))"
    echo "   why (error lines from its whole log):"
    grep -iE "error|assert|abort|exception|terminate|out of memory|failed|cannot|invalid" "$LOG" | grep -vE "\(\+0x|\[0x[0-9a-f]+\][[:space:]]*$" | tail -12 | sed 's/^/   | /'
    echo "   its last lines:"
    tail -8 "$LOG" | sed 's/^/   | /'
    echo "   kernel, last minute:"
    dmesg -T 2>/dev/null | tail -200 | grep -iE "llama|segfault|oom|killed process|NVRM|Xid" | tail -5 | sed 's/^/   | /'
    exit 2
}

printf "== loading "
for i in $(seq 1 300); do
    alive || died
    if curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/health" 2>/dev/null | grep -q 200; then break; fi
    printf "."; sleep 1
done
echo " ready in $(( $(date +%s) - t0 )) s"
echo "   what it said about the GPU and the projector:"
if ! grep -iE "build|cuda|gpu|device|offload|buffer|vram|clip|mtmd|vision|audio|projector|warn|error" "$LOG" | head -30 | sed 's/^/   | /' | grep .; then
    echo "   (nothing about the GPU in its log; its first lines, so the parser can learn this build's format:)"
    head -15 "$LOG" | sed 's/^/   | /'
fi

ask() { # name, body file
    local name="$1" t1 code
    t1=$(date +%s%N)
    code=$(curl -s -o "$OUT" -w '%{http_code}' --max-time 300 -H 'Content-Type: application/json' \
        --data-binary @"$BODY" "http://127.0.0.1:$PORT/v1/chat/completions")
    printf "== %-26s http %s in %s ms\n" "$name" "$code" "$(( ($(date +%s%N) - t1) / 1000000 ))"
    if [ "$code" = 000 ] || [ "$code" = 100 ]; then
        echo "   the connection dropped mid-request"
        for _ in 1 2 3 4 5 6; do alive || died; sleep 0.5; done
    fi
    alive || died
    if [ "$code" = 200 ]; then
        printf "   answer: %s\n" "$(grep -o '"content":"[^"]*' "$OUT" | head -1 | cut -c12- | cut -c1-220)"
        printf "   speed:  %s\n" "$(grep -o '"predicted_per_second":[0-9.]*' "$OUT" | head -1 | cut -d: -f2) tokens/s"
    else
        printf "   said:   %s\n" "$(head -c 400 "$OUT")"
    fi
}
photo() { # name, base64 file
    { printf '{"messages":[{"role":"user","content":[{"type":"text","text":"Describe this photo in two sentences."},{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,'
      cat "$2"
      printf '"}}]}],"max_tokens":160,"chat_template_kwargs":{"enable_thinking":false}}'; } > "$BODY"
    ask "$1" "$BODY"
}

printf '{"messages":[{"role":"user","content":"In one sentence: what is a lighthouse for?"}],"max_tokens":80,"chat_template_kwargs":{"enable_thinking":false}}' > "$BODY"
ask "text" "$BODY"

B64="$(mktemp /tmp/llama-probe.XXXXXX.b64)"
printf '%s' "/9j/2wCEAAYEBQYFBAYGBQYHBwYIChAKCgkJChQODwwQFxQYGBcUFhYaHSUfGhsjHBYWICwgIyYnKSopGR8tMC0oMCUoKSgBBwcHCggKEwoKEygaFhooKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKP/AABEIAEAAYAMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AMULTwtPC08LX3LkfFwkMC08LTwtSBahyOmEiMLUgWnYCjJIA96j+0xg/wAR9wK48RjaGGt7WaVz1sFgMVjL/V6blbsv1JQtPC1Ct3FkZDD3xVuIq65Qgj2rOjjqGIdqU02dVfL8Xg0pV6bin3Q0LTwtSBaeFrdyMoSGBaeFp4WnhahyOmEjCC08LTwtSBa6XI+KhIYFp2AqknoOTUgWoNQytuMd2ANceMxP1ehOra9kevlWF+vYunhr25mlfy6lKaUyMeTt7Co6KK/MK1adabqVHds/oLDYanhaao0VaKCnwyvC+5Dj1HY0yiphOVOSlB2aLq0oVYOnUV090zooGWWNXT7pqYLWdoJJWZf4QQQPrn/CtcLX6LgcU8Vh41Xu/wBND8ZzTCLAYyph4u6i9PRq6/BjAtPC08LUgWulyOeEjCC08LTwtPC10uR8TCQwLVbVFP2YEDowz+tXwtLJCssbIw4YYrjxtJ4ihOkt2j2snx0cDjKWJkrqLTfp1OaoqS5ha3maNweOh9R61HX5pOEqcnGSs0f0TRrQr041abvFq6fkFFFOijeaRY41LOxwAKlJydkXKSgnKTskbHh1SROcHHyjP51thajsbVbW3WJecck4xk1aC1+gYCi8Nh4Upbr9dT8SzjHwx2OqYin8Lenoklf52uMC08LTwtPC11ORyQkYQWnhakC08LXS5HxUJDAtPC08LTwtQ5HTCRBLbxzx7JVDL1xWc2hKWOychewK5P8AOtsLUgWuHE4PD4l3qxu/67HvZbnuPy5OOFquKfTRr7mmr+ZgpoA3DdcErnkBMf1rXs7OK0TbCuM9SeSatBaeFrOhgcPh3zUo2f3/AJnVjM/x+YQ9niarce2iXzSSv8xgWnhaeFp4WuhyOKEhgWnhaeFqQLUOR0wkf//Z" > "$B64"
photo "tiny photo (red disc)" "$B64"

REAL="$(find "$R$M/frames" -type f -iname '*.jpg' -size +200k -size -8M -print -quit 2>/dev/null)"
FF="$(command -v ffmpeg || true)"; FFLIB=""
if [ -z "$FF" ] && [ -x "$R$M/runtime/ffmpeg/bin/ffmpeg" ]; then FF="$R$M/runtime/ffmpeg/bin/ffmpeg"; FFLIB="$R$M/runtime/ffmpeg/lib"; fi
if [ -n "$REAL" ]; then
    DIM="$(LD_LIBRARY_PATH="$FFLIB" "${FF%ffmpeg}ffprobe" -v error -select_streams v:0 -show_entries stream=width,height -of csv=p=0 "$REAL" 2>/dev/null | head -1)"
    if [ -n "$FF" ]; then
        SMALLJPG="$(mktemp /tmp/llama-probe.XXXXXX.jpg)"
        if LD_LIBRARY_PATH="$FFLIB" "$FF" -loglevel error -y -i "$REAL" -vf "scale='if(gt(iw,ih),min(1024,iw),-2)':'if(gt(iw,ih),-2,min(1024,ih))'" -q:v 3 -f mjpeg "$SMALLJPG" 2>/dev/null; then
            base64 -w0 "$SMALLJPG" > "$B64"
            photo "same photo at 1024 px" "$B64"
        fi
        rm -f "$SMALLJPG"
    fi
    base64 -w0 "$REAL" > "$B64"
    photo "the photo as is ($(du -h "$REAL" | cut -f1)${DIM:+, ${DIM/,/x}})" "$B64"
else
    echo "== a real photo: none found under $M/frames"
fi
rm -f "$B64"
echo "== still alive after all three. Its last lines:"
tail -6 "$LOG" | sed 's/^/   | /'
