#!/bin/sh
# bundle_db_runtime.sh , copy the Postgres and Redis runtimes onto the ENCRYPTED volume, so the OS
# disk can stop carrying database software entirely. Run as the service user, with the volume
# UNLOCKED, after the first successful unlock (the first-ever initdb bootstraps from the OS packages;
# everything after runs from the volume).
#
# What it does:
#   1. Mirrors Debian's Postgres tree , usr/lib/postgresql/<ver>/{bin,lib} AND
#      usr/share/postgresql/<ver> , into <mount>/runtime/pgroot. The STRUCTURE must travel intact:
#      Postgres relocates by relative offset from where the binary actually sits, so bin/lib/share
#      keep their Debian geometry. pgvector's vector.so and extension files live inside that tree and
#      ride along for free.
#   2. Copies redis-server and redis-cli into <mount>/runtime/redis/bin.
#   3. Walks ldd for every binary and copies the shared-library closure into runtime lib dirs ,
#      the daemons run them with LD_LIBRARY_PATH pointed there, so an OS library upgrade cannot break
#      the volume's databases. (Honest limit: the dynamic loader and glibc itself stay the system's ,
#      full isolation would need a chroot, and glibc ABI stability is what makes this cut safe.)
#   4. --verify: runs the bundled initdb into a throwaway dir ON the volume, using ONLY the bundle,
#      and deletes it. If that passes, the OS packages are removable.
#
# After a successful --verify, the box no longer needs the OS packages for RUNTIME:
#     apt-get remove postgresql postgresql-<ver> postgresql-<ver>-pgvector redis-server redis-tools
# (re-running server_setup_root.sh would reinstall them , that script is for bootstrap; skip its
# package step on a bundled box or just let it reinstall, nothing conflicts.)
#
# Re-run any time to refresh the bundle (e.g. after choosing to take a deliberate PG upgrade ,
# which is a data migration you plan, not something apt does to you overnight).

set -eu

MOUNT="${1:-}"
VERIFY=0
[ "${2:-}" = "--verify" ] && VERIFY=1
if [ -z "$MOUNT" ] || [ ! -d "$MOUNT" ]; then
    echo "usage: $0 <mount-path> [--verify]     e.g. $0 /var/lib/ghost/mnt/slot0 --verify"
    exit 2
fi
if [ ! -w "$MOUNT" ]; then
    echo "ERROR: $MOUNT not writable (volume locked, or wrong user?)"
    exit 1
fi

RT="$MOUNT/runtime"
PGBIN_SRC="$(ls -d /usr/lib/postgresql/*/bin 2>/dev/null | sort -V | tail -1 || true)"
if [ -z "$PGBIN_SRC" ]; then
    echo "ERROR: no OS Postgres found to bundle from (install postgresql first; bundling copies FROM it)"
    exit 1
fi
PGVER_DIR="$(dirname "$PGBIN_SRC")"                       # /usr/lib/postgresql/<ver>
PGVER="$(basename "$PGVER_DIR")"
PGSHARE_SRC="/usr/share/postgresql/$PGVER"

echo "> Bundling Postgres $PGVER + Redis into $RT ..."
mkdir -p "$RT/pgroot/usr/lib/postgresql" "$RT/pgroot/usr/share/postgresql" "$RT/pgroot/lib" \
         "$RT/redis/bin" "$RT/redis/lib"

cp -a "$PGVER_DIR"   "$RT/pgroot/usr/lib/postgresql/"
cp -a "$PGSHARE_SRC" "$RT/pgroot/usr/share/postgresql/"
if [ -f "$RT/pgroot/usr/lib/postgresql/$PGVER/lib/vector.so" ]; then
    echo "  pgvector rode along (vector.so present in the bundled tree)"
else
    echo "  NOTE: pgvector not in the bundled tree , search will run FTS-only from this bundle"
fi

for b in redis-server redis-cli; do
    src="$(command -v "$b" || true)"
    [ -z "$src" ] && { echo "ERROR: $b not installed to bundle from"; exit 1; }
    cp -a "$src" "$RT/redis/bin/"
done

# Shared-library closure: every "=> /path" line of ldd, deduped, copied. Skips the loader and vdso.
collect_libs() {  # collect_libs <dest-libdir> <binary>...
    dest="$1"; shift
    for bin in "$@"; do
        ldd "$bin" 2>/dev/null | awk '/=> \//{print $3}' | while read -r lib; do
            [ -f "$dest/$(basename "$lib")" ] || cp -a "$lib" "$dest/"
        done
    done
}
echo "> Collecting shared-library closures..."
collect_libs "$RT/pgroot/lib" "$RT/pgroot/usr/lib/postgresql/$PGVER/bin/"*
collect_libs "$RT/redis/lib"  "$RT/redis/bin/"*

printf 'pg=%s\nbundled=%s\n' "$PGVER" "$(date -u +%FT%TZ)" > "$RT/VERSION"
echo "  bundle written ($(du -sh "$RT" | cut -f1))"

if [ "$VERIFY" = 1 ]; then
    echo "> Verifying: bundled initdb into a throwaway dir on the volume, OS packages not consulted..."
    VD="$RT/.verify.$$"
    if LD_LIBRARY_PATH="$RT/pgroot/usr/lib/postgresql/$PGVER/lib:$RT/pgroot/lib" \
        "$RT/pgroot/usr/lib/postgresql/$PGVER/bin/initdb" -D "$VD" --auth=trust --encoding=UTF8 >/dev/null 2>&1; then
        rm -rf "$VD"
        echo "  VERIFIED , the volume runtime stands alone. The OS database packages are now removable:"
        echo "    apt-get remove postgresql 'postgresql-*' redis-server redis-tools"
    else
        rm -rf "$VD"
        echo "  VERIFY FAILED , keep the OS packages; the daemons fall back to them automatically."
        exit 1
    fi
fi
echo "> Done. ghost.secd prefers $RT automatically on the next unlock; no config needed."
