#!/usr/bin/env bash
# cut_release.sh <version> [--apk <file>] , cut a release, or cut it again from the same place.
#
#   ./tools/cut_release.sh 0.0.1                          the first time: tags HEAD as v0.0.1 and builds from the tag
#   ./tools/cut_release.sh 0.0.1 --apk ~/app-release.apk  with the app, built at the same commit (below)
#   ./tools/cut_release.sh 0.0.1                          again: builds from the tag v0.0.1, the same bytes
#
# ONE RELEASE for the whole of LocalGhost, release/<version>/:
#   server/   the mirror's server set (the bundle, RELEASE.txt, NOTES.md, NOTICE.txt, TERMS-MIT.txt):
#             what a box takes from the phone (tools/release_build.sh, reproducible)
#   app/      the Android app, localghost-app-<version>.apk, built here from the tag's tree when the
#             Android SDK is on this machine (ANDROID_HOME, or ~/.localghost_android_env from
#             app/android/tools/debian_setup.sh) and signed with the app's keystore (LG_KEYSTORE and
#             LG_KEY_ALIAS, kept in ~/.config/localghost/release.env; LG_KEYSTORE_PASS too, or apksigner
#             asks); else handed in with --apk from a machine that built it at the same commit. With
#             its GPG signature when the site key is in this user's gpg (info@localghost.ai), and
#             APP.txt (its hash, commit, version and signing certificate). --no-apk leaves it out
#   source/   localghost-<version>-source.tar.gz, git archive of the tag: the whole tree, which is
#             also how a box is set up (server/tools/setup.sh, see server/tools/README.md)
#   SHA256SUMS over all of it, and SHA256SUMS.asc when the site key is here
# The GitHub release v<version> carries every file; the mirror's server set takes server/.
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

VERSION="${1:?usage: cut_release.sh <version> [--apk <file> | --no-apk]}"
shift
APK=""
NOAPK=""
while [ $# -gt 0 ]; do
    case "$1" in
        --apk) APK="${2:?--apk needs a file}"; shift 2 ;;
        --no-apk) NOAPK=1; shift ;;
        *) echo "unknown option $1 (--apk <file> | --no-apk)" >&2; exit 2 ;;
    esac
done
if [ -n "$APK" ]; then
    [ -s "$APK" ] || { echo "no APK at $APK" >&2; exit 2; }
    APK="$(cd "$(dirname "$APK")" && pwd)/$(basename "$APK")"
    # an APK is a zip with the app's manifest and code in it
    unzip -l "$APK" 2>/dev/null | grep -qE ' AndroidManifest\.xml$' && unzip -l "$APK" | grep -qE ' classes[0-9]*\.dex$' ||
        { echo "$APK is not an Android app (no AndroidManifest.xml and classes.dex in it)" >&2; exit 2; }
fi
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
# where this folder sits in the repository ("server/" when the module is a folder of it, "" when
# it is the repository): the worktree of the tag is the whole repository, the build runs here
PREFIX="$(git rev-parse --show-prefix)"
# Go, for the build: on PATH, else the system Go setup installs
GO="${GO:-$(command -v go 2>/dev/null || true)}"
[ -x "${GO:-/nonexistent}" ] || GO=/usr/local/go/bin/go
[ -x "$GO" ] || { echo "no go on PATH and none at /usr/local/go/bin/go" >&2; exit 2; }

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
git worktree add --detach "$W/src" "$TAG" >/dev/null 2>&1 || { echo "could not make a worktree of $TAG" >&2; exit 1; }
rm -rf "$OUT"
mkdir -p "$OUT/source"
( cd "$W/src/$PREFIX" && GO="$GO" ./tools/release_build.sh "$VERSION" "$OUT" )
# the site key, for the signatures (info@localghost.ai; the key that signs the site and the mirror)
SIGNER="${GHOST_RELEASE_SIGNER:-info@localghost.ai}"
sign() { # sign <file>: <file>.asc when the key is in this user's gpg, said once when it is not
    if gpg --batch --list-secret-keys "$SIGNER" >/dev/null 2>&1; then
        gpg --batch --yes --armor --local-user "$SIGNER" --output "$1.asc" --detach-sign "$1"
    else
        [ -n "${nosign:-}" ] || echo "note: no secret key for $SIGNER in this user's gpg: nothing is GPG-signed (run the cut as the user that holds it, or sign SHA256SUMS by hand)"
        nosign=1
    fi
}
# THE APP. Built here from the tag's tree when the Android SDK is on this machine, else handed in.
APPOUT="$OUT/app/localghost-app-$VERSION.apk"
BT=""
build_apk() {
    [ -f "$HOME/.config/localghost/release.env" ] && . "$HOME/.config/localghost/release.env"
    if [ -z "${ANDROID_HOME:-}" ] && [ -f "$HOME/.localghost_android_env" ]; then
        . "$HOME/.localghost_android_env"
    fi
    if [ -z "${ANDROID_HOME:-}" ] || [ ! -d "$ANDROID_HOME" ]; then
        echo "no Android SDK on this machine (ANDROID_HOME; app/android/tools/debian_setup.sh installs one): the app is not built here, --apk hands one in"
        return 1
    fi
    if [ -z "${LG_KEYSTORE:-}" ] || [ ! -s "$LG_KEYSTORE" ] || [ -z "${LG_KEY_ALIAS:-}" ]; then
        echo "no keystore for the app: put LG_KEYSTORE=<file.jks> and LG_KEY_ALIAS=<alias> in ~/.config/localghost/release.env (keytool -genkeypair makes one; it is the app's identity, keep it)"
        return 1
    fi
    BT="$(ls -d "$ANDROID_HOME"/build-tools/*/ 2>/dev/null | sort -V | tail -1)"
    BT="${BT%/}"
    [ -x "$BT/apksigner" ] && [ -x "$BT/zipalign" ] || { echo "no build-tools under $ANDROID_HOME"; return 1; }
    APPDIR="$W/src/app/android"
    [ -x "$APPDIR/gradlew" ] || { echo "no app/android/gradlew in the tag"; return 1; }
    # the public build: no box baked in; the SDK; the pinned llama.cpp tarball where the box has it
    {
        echo "sdk.dir=$ANDROID_HOME"
        echo "NAS_BASE_URL="
        echo "DEVICE_TOKEN="
        [ -n "${LG_LLAMA_TARBALL:-}" ] && echo "llamaTarball=$LG_LLAMA_TARBALL"
    } > "$APPDIR/local.properties"
    echo "building the app (gradle assembleRelease; the phone's model runtime takes a few minutes the first time)"
    ( cd "$APPDIR" && ./gradlew --no-daemon -q assembleRelease ) || { echo "the app's build failed (above)"; return 1; }
    UNSIGNED="$(ls "$APPDIR"/app/build/outputs/apk/release/*.apk 2>/dev/null | head -1)"
    [ -s "$UNSIGNED" ] || { echo "no APK came out of the build"; return 1; }
    mkdir -p "$OUT/app"
    "$BT/zipalign" -f 4 "$UNSIGNED" "$W/aligned.apk"
    "$BT/apksigner" sign --ks "$LG_KEYSTORE" --ks-key-alias "$LG_KEY_ALIAS" ${LG_KEYSTORE_PASS:+--ks-pass env:LG_KEYSTORE_PASS} \
        --out "$APPOUT" "$W/aligned.apk" || { echo "signing the app failed"; return 1; }
    rm -f "$W/aligned.apk"
    echo "the app built and signed: $APPOUT"
}
if [ -z "$NOAPK" ]; then
    if [ -n "$APK" ]; then
        mkdir -p "$OUT/app"
        cp "$APK" "$APPOUT"
        [ -s "$APK.asc" ] && cp "$APK.asc" "$APPOUT.asc"
    else
        build_apk || true
    fi
fi
if [ -s "$APPOUT" ]; then
    [ -s "$APPOUT.asc" ] || sign "$APPOUT"
    # what the APK says of itself, when the build tools are here to ask
    [ -n "$BT" ] || { BT="$(ls -d "${ANDROID_HOME:-/nonexistent}"/build-tools/*/ 2>/dev/null | sort -V | tail -1)"; BT="${BT%/}"; }
    CERT=""; VN=""; VC=""
    if [ -n "$BT" ] && [ -x "$BT/apksigner" ]; then
        CERT="$("$BT/apksigner" verify --print-certs "$APPOUT" 2>/dev/null | grep -i 'certificate SHA-256' | head -1 | awk '{print $NF}')"
    fi
    if [ -n "$BT" ] && [ -x "$BT/aapt2" ]; then
        BADGE="$("$BT/aapt2" dump badging "$APPOUT" 2>/dev/null | head -1)"
        VN="$(printf '%s' "$BADGE" | sed -n "s/.*versionName='\([^']*\)'.*/\1/p")"
        VC="$(printf '%s' "$BADGE" | sed -n "s/.*versionCode='\([^']*\)'.*/\1/p")"
        if [ -n "$VN" ] && [ "$VN" != "$VERSION" ]; then
            echo "the APK says it is version $VN, the release is $VERSION (app/android/app/build.gradle.kts versionName): not a release" >&2
            exit 1
        fi
    fi
    {
        echo "app=localghost-app-$VERSION.apk"
        echo "version=$VERSION"
        echo "name=$NAME"
        echo "commit=$COMMIT"
        echo "sha256=$(sha256sum "$APPOUT" | cut -d' ' -f1)"
        [ -n "$VN" ] && echo "versionName=$VN"
        [ -n "$VC" ] && echo "versionCode=$VC"
        [ -n "$CERT" ] && echo "signingCertSha256=$CERT"
        echo "built=from the tree at commit $COMMIT, signed with the app's keystore; the signing certificate is what the app shows under VERIFY BUILD"
    } > "$OUT/app/APP.txt"
fi
# the whole tree at the tag: the source, and how a box is set up from it
( cd "$W/src" && git archive --format=tar.gz --prefix="localghost-$VERSION/" -o "$OUT/source/localghost-$VERSION-source.tar.gz" "$TAG" )
# the sums over everything, signed
cd "$OUT"
find . -type f ! -name SHA256SUMS ! -name SHA256SUMS.asc | sed 's|^\./||' | LC_ALL=C sort | xargs sha256sum > SHA256SUMS
sign SHA256SUMS
say "$NAME $VERSION ($COMMIT) in $OUT"
cat SHA256SUMS
[ -s "$APPOUT" ] || echo "(no app in this cut: the server set and the source are complete without it; the app is built here with the SDK and a keystore, or handed in with --apk)"
echo
echo "next:"
echo "  git push origin $TAG"
FILES="$(cd "$OUT" && find . -type f | sed "s|^\./|$OUT/|" | LC_ALL=C sort | tr '\n' ' ')"
if command -v gh >/dev/null 2>&1; then
    echo "  gh release create $TAG --title \"$NAME $VERSION\" --notes-file $HERE/$NOTES $FILES"
else
    echo "  the GitHub release, from a machine with gh (sudo apt install gh puts it on this one):"
    echo "    gh release create $TAG --title \"$NAME $VERSION\" --notes-file $NOTES \$(find release/$VERSION -type f)"
    echo "  or in the browser: https://github.com/LocalGhostDao/localghost/releases/new?tag=$TAG"
    echo "    title \"$NAME $VERSION\", the notes from $NOTES, every file under $OUT attached"
fi
echo "  then point the mirror's server set at the release's server files (the web repo's mirror.conf) and publish"
echo
echo "to cut it again anywhere: git clone … && ./tools/cut_release.sh $VERSION (the same bytes, from $TAG)"
