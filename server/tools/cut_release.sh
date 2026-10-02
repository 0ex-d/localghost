#!/usr/bin/env bash
# cut_release.sh <version> , cut a release, or cut it again from the same place.
#
#   ./tools/cut_release.sh 0.0.1          the first time: tags HEAD as v0.0.1 and builds from the tag
#   ./tools/cut_release.sh 0.0.1          again: builds from the tag v0.0.1, the same bytes
#
# A release is three things committed with the code, so cutting it twice gives the same files:
#   tools/release.names      "<version> <name>" (0.0.1 is wisp): the name the phone shows
#   releases/<version>.md    what the release does, what is in it, how it works: the full notes,
#                            which travel in the bundle (NOTES.md) and in the mirror's set
#   releases/pins.txt        "<version> <commit>", written at the first cut: the pin. A later cut
#                            refuses to build when the tag no longer points at the pinned commit.
# The build itself is tools/release_build.sh (reproducible), run in a clean worktree of the tag, so
# the state of the checkout the command runs in does not matter. The output lands in
# release/<version>/server/: the bundle, RELEASE.txt, NOTES.md, NOTICE.txt, TERMS-MIT.txt, and
# SHA256SUMS. The first cut prints what to do next (the GitHub release, the mirror's server set).
set -euo pipefail

VERSION="${1:?usage: cut_release.sh <version>}"
case "$VERSION" in *[!0-9.]*) echo "a version is numbers and dots (0.0.1); the name comes from tools/release.names" >&2; exit 2 ;; esac
HERE="$(cd "$(dirname "$0")/.." && pwd)"
cd "$HERE"
TAG="v$VERSION"
NOTES="releases/$VERSION.md"
PINS="releases/pins.txt"
OUT="${GHOST_RELEASE_OUT:-$HERE/release/$VERSION}"

NAME="$(awk -v v="$VERSION" '$1 == v { print $2; exit }' tools/release.names 2>/dev/null || true)"
[ -n "$NAME" ] || { echo "no name for $VERSION in tools/release.names (a line: \"$VERSION <name>\")" >&2; exit 2; }
[ -s "$NOTES" ] || { echo "no notes at $NOTES: write what the release does, what is in it and how it works, and commit it" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "not a git checkout" >&2; exit 2; }

say() { printf '\n== %s ==\n' "$*"; }

if git rev-parse -q --verify "refs/tags/$TAG^{commit}" >/dev/null; then
    COMMIT="$(git rev-parse "$TAG^{commit}")"
    PINNED="$(awk -v v="$VERSION" '$1 == v { print $2; exit }' "$PINS" 2>/dev/null || true)"
    if [ -n "$PINNED" ] && [ "$PINNED" != "$COMMIT" ]; then
        echo "the tag $TAG points at $COMMIT but $PINS pins $VERSION to $PINNED: the tag was moved. Put it back (git tag -f $TAG $PINNED) or pin the new commit on purpose." >&2
        exit 1
    fi
    [ -n "$PINNED" ] || echo "note: $PINS has no line for $VERSION; the tag $TAG is the pin ($COMMIT)"
    say "cutting $NAME $VERSION again from $TAG ($COMMIT)"
else
    # the first cut: from a clean tree, the notes and the name committed
    if [ -n "$(git status --porcelain)" ]; then
        echo "the tree has uncommitted changes: a release is cut from a commit (commit $NOTES and tools/release.names with the code first)" >&2
        exit 1
    fi
    for f in "$NOTES" tools/release.names; do
        git ls-files --error-unmatch "$f" >/dev/null 2>&1 || { echo "$f is not committed" >&2; exit 1; }
    done
    COMMIT="$(git rev-parse HEAD)"
    say "cutting $NAME $VERSION from $COMMIT: tagging $TAG"
    # the first paragraph of the notes is the tag's message
    MSG="$(awk 'NR == 1 { next } /^$/ { if (n) exit; next } { print; n = 1 }' "$NOTES")"
    git tag -a "$TAG" -m "$NAME $VERSION" -m "$MSG" "$COMMIT"
    mkdir -p releases
    echo "$VERSION $COMMIT" >> "$PINS"
    echo "pinned: $VERSION $COMMIT in $PINS (commit it: git add $PINS && git commit -m 'pin $NAME $VERSION')"
fi

# the build, in a worktree of the tag: whatever this checkout holds, the release is the tag's
W="$(mktemp -d)"
cleanup() { git worktree remove --force "$W/src" >/dev/null 2>&1 || true; rm -rf "$W"; }
trap cleanup EXIT
git worktree add --detach "$W/src" "$TAG" >/dev/null 2>&1
rm -rf "$OUT"
mkdir -p "$OUT"
( cd "$W/src" && ./tools/release_build.sh "$VERSION" "$OUT" )
cd "$OUT/server"
sha256sum ./* | sed 's|  \./|  |' > SHA256SUMS
say "$NAME $VERSION ($COMMIT) in $OUT/server"
cat SHA256SUMS
cat <<EOF

next:
  git push origin $TAG
  gh release create $TAG --title "$NAME $VERSION" --notes-file $HERE/$NOTES $OUT/server/*
  then point the mirror's server set at the release's files (the web repo's mirror.conf) and publish

to cut it again anywhere: git clone … && ./tools/cut_release.sh $VERSION (the same bytes, from $TAG)
EOF
