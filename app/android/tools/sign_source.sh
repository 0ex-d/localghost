#!/usr/bin/env bash
# Sign the LocalGhost app source, the same convention as the website deploy-manifest. Run on a
# CLEAN git tree, before the build (app/android/tools/release.sh runs it).
#
#   tools/sign_source.sh
#
# Produces (under ghost/, mirroring the website layout):
#   ghost/source-manifest.txt       header + "sha256  path" for every tracked source file
#   ghost/source-manifest.txt.asc   detached GPG signature (info@localghost.ai)
#   MANIFEST.root                    sha256 of source-manifest.txt (stamped into BuildConfig)
set -euo pipefail
# the app's folder (app/android of the LocalGhost repository): the manifest lists the app's tracked
# files, paths relative to here, and MANIFEST.root beside them is what the build stamps in
cd "$(dirname "$0")/.."

BUILD_ID="$(git rev-parse HEAD)"
mkdir -p ghost
MANIFEST_FILE="ghost/source-manifest.txt"
MANIFEST_SIG="ghost/source-manifest.txt.asc"

{
    echo "# LocalGhost App Source Manifest"
    echo "# Build: ${BUILD_ID}"
    echo "# Signed: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo ""
    # Every tracked file except build outputs and the manifest itself, hashed and sorted by path.
    git ls-files \
      | grep -vE '^(ghost/source-manifest|MANIFEST\.root|.*/build/|build/)' \
      | LC_ALL=C sort \
      | while IFS= read -r f; do [ -f "$f" ] && sha256sum "$f"; done \
      | sed "s|  | |" | sort -k2
} > "$MANIFEST_FILE"

gpg --batch --yes --armor --local-user info@localghost.ai \
    --output "$MANIFEST_SIG" --detach-sign "$MANIFEST_FILE"

# Root hash = sha256 of the manifest, stamped into the APK via BuildConfig.MANIFEST_ROOT.
sha256sum "$MANIFEST_FILE" | awk '{print $1}' > MANIFEST.root

FILE_COUNT=$(grep -c "^[a-f0-9]" "$MANIFEST_FILE")
echo "  [🔏] source-manifest.txt (${FILE_COUNT} files)"
echo "  [🔏] source-manifest.txt.asc"
echo "  [#]  MANIFEST.root: $(cat MANIFEST.root)"
