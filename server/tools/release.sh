#!/usr/bin/env bash
# LocalGhost backend release. Two clean deploy paths, no pkill, nothing orphaned , because ghost.watchd
# (on the encrypted volume) owns the daemon lifecycle and ghost.secd (the only thing on the system
# disk) owns exactly one child, watchd.
#
#   DAEMON deploy (ghost.*d): drop the new binary at <mount>/bin/<name>, then ask watchd to restart
#   it (ghost-ctl restart-daemon). The box stays unlocked and mounted throughout. secd is untouched.
#
#   SECD deploy: secd has ONE shutdown behaviour , SIGTERM does a clean full lock (stop watchd ->
#   cohort torn down + confirmed dead -> stop DBs -> unmount -> close LUKS). So a secd deploy is
#   `systemctl restart ghost.secd` and then a RE-UNLOCK from the app. Nothing orphans; the trade is
#   one re-unlock per secd deploy, which is correct for a security appliance.
#
# Usage:
#   ./release.sh --daemon <name>     # build one daemon, install to <mount>/bin, watchd restarts it
#   ./release.sh --secd              # build + install secd, restart the unit (box will re-lock)
#   ./release.sh --all               # build everything; install daemons to volume, then (if secd
#                                    #   changed) restart secd. Box re-locks only if secd changed.
#   ./release.sh --dry-run           # show the plan, touch nothing
set -uo pipefail

BINDIR=bin
SYSBIN=/usr/local/bin                 # secd lives here (systemd runs it)
MOUNT=/var/lib/ghost/mnt/slot0        # daemon binaries live at $MOUNT/bin
VOLBIN="$MOUNT/bin"
MODE=""; TARGET=""; DRY=0
while [ $# -gt 0 ]; do case "$1" in
  --daemon) MODE=daemon; TARGET="${2:-}"; shift 2;;
  --secd)   MODE=secd; shift;;
  --all)    MODE=all; shift;;
  --dry-run) DRY=1; shift;;
  *) echo "unknown flag: $1"; exit 2;; esac; done
[ -z "$MODE" ] && { echo "pick one: --daemon <name> | --secd | --all"; exit 2; }

run() { echo "+ $*"; [ "$DRY" = 1 ] || "$@"; }
mounted() { mountpoint -q "$MOUNT" 2>/dev/null; }

case "$MODE" in
daemon)
  [ -z "$TARGET" ] && { echo "--daemon needs a name, e.g. ghost.synthd"; exit 2; }
  echo "== building $TARGET =="
  run go build -o "$BINDIR/" "./cmd/$TARGET" || { echo "build failed"; exit 1; }
  if ! mounted; then
    echo "box is LOCKED , cannot install to the volume. Unlock first, or the binary will be seeded"
    echo "on next provision. To deploy now, unlock the box and re-run."
    exit 1
  fi
  echo "== installing $TARGET to $VOLBIN and asking watchd to restart it =="
  run sudo install -m 0750 "$BINDIR/$TARGET" "$VOLBIN/$TARGET"
  run sudo chown ghost:ghost "$VOLBIN/$TARGET" || true   # match volume ownership (adjust for --user)
  run ghost-ctl restart-daemon "$TARGET"
  echo "== done: $TARGET restarted from the new binary, box stayed unlocked =="
  ;;

secd)
  echo "== building ghost.secd + operator tools (ghost-cli, ghost-ctl) =="
  run go build -o "$BINDIR/" ./cmd/ghost.secd ./cmd/ghost-cli ./cmd/ghost-ctl || { echo "build failed"; exit 1; }
  echo "== installing ghost.secd and restarting the unit =="
  echo "   NOTE: restarting secd does a clean full lock. The box will be LOCKED after this."
  echo "   Re-unlock from the app once it comes back up."
  run sudo install -m 0755 "$BINDIR/ghost.secd" "$SYSBIN/ghost.secd"
  # ghost-cli + ghost-ctl live on the UNENCRYPTED disk with secd: they are operator tools you want
  # available even when the box is locked (e.g. `ghost-cli ghost.secd status`).
  run sudo install -m 0755 "$BINDIR/ghost-cli" "$SYSBIN/ghost-cli"
  run sudo install -m 0755 "$BINDIR/ghost-ctl" "$SYSBIN/ghost-ctl"
  run sudo systemctl restart ghost.secd
  run sudo systemctl --no-pager status ghost.secd | head -5 || true
  echo "== done: ghost.secd + tools updated. RE-UNLOCK from the app. =="
  ;;

all)
  echo "== building everything =="
  run go build -o "$BINDIR/" ./cmd/... || { echo "build failed"; exit 1; }
  # Install the daemons to the volume (needs the box unlocked); install secd to the system dir.
  if mounted; then
    echo "== installing daemons to $VOLBIN =="
    for b in ghost.watchd ghost.shadowd ghost.framed ghost.noted ghost.synthd ghost.tallyd ghost.voiced ghost.cued; do
      [ -f "$BINDIR/$b" ] || continue
      run sudo install -m 0750 "$BINDIR/$b" "$VOLBIN/$b"
      run sudo chown ghost:ghost "$VOLBIN/$b" || true
    done
    echo "   (restart individual daemons with: ghost-ctl restart-daemon <name>)"
  else
    echo "box LOCKED , skipping volume daemon install (they seed on next provision, or unlock + rerun)"
  fi
  # secd: only restart if the binary changed (a re-lock is disruptive; do not force it needlessly).
  if [ -f "$SYSBIN/ghost.secd" ] && [ -f "$BINDIR/ghost.secd" ] \
     && [ "$(sha256sum "$BINDIR/ghost.secd" | cut -d' ' -f1)" = "$(sha256sum "$SYSBIN/ghost.secd" | cut -d' ' -f1)" ]; then
    echo "== ghost.secd unchanged , not restarting =="
  else
    echo "== ghost.secd changed , installing + restarting (box will RE-LOCK) =="
    run sudo install -m 0755 "$BINDIR/ghost.secd" "$SYSBIN/ghost.secd"
    run sudo systemctl restart ghost.secd
    echo "   RE-UNLOCK from the app."
  fi
  ;;
esac
