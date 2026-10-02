#!/usr/bin/env bash
# apply_removed.sh , deletes the files a drop removed (tools/removed.txt), so a tree a drop was
# copied over holds what the drop holds. Run from anywhere: on the box redeploy.sh runs it before
# the build; on Windows run it once in Git Bash after unpacking a drop, then commit, and git takes
# the files out for the box too:
#
#   bash server/tools/apply_removed.sh && git add -A
#
# Only a file still holding the drop's stand-in ("left empty so a tree") is deleted; a file with
# real content at a listed path is reported and left alone.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LIST="$(dirname "$0")/removed.txt"
[ -f "$LIST" ] || { echo "removed: no list at $LIST"; exit 0; }

gone=0 kept=0
while IFS= read -r line || [ -n "$line" ]; do
    p="${line%$'\r'}"
    case "$p" in ''|'#'*) continue ;; esac
    case "$p" in /*|*..*) echo "removed: skipped an unsafe path: $p"; continue ;; esac
    f="$ROOT/$p"
    [ -e "$f" ] || continue
    if grep -q "left empty so a tree" "$f" 2>/dev/null; then
        rm -f "$f"
        echo "removed: $p"
        gone=$((gone + 1))
    else
        echo "removed: $p has content of its own, left alone"
        kept=$((kept + 1))
    fi
done < "$LIST"
[ "$gone" = 0 ] && [ "$kept" = 0 ] && echo "removed: nothing to take out"
exit 0
