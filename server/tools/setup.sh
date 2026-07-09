#!/bin/sh
# setup.sh , the guided LocalGhost bring-up. ONE command, run as root, that walks the whole flow:
# verify the box prep, build the binaries AS THE SERVICE USER, let you pick the disk from a list,
# choose the seal tier, dry-run, and (only on an explicit typed confirmation) provision.
#
# Why root, not the service user: privilege drops, it does not round-trip. A script cannot become the
# user, build, and come back to root in one process. So this runs as root and uses `sudo -u <user>`
# for the build step , the one part that must run in the user's context , keeping provisioning
# (which writes the disk and mints the CA) in root's own hands. One uninterrupted run, no manual
# su-ing back and forth.
#
# It changes nothing until you confirm. The disk step and the apply step each require typing an
# explicit word , there is no --apply-by-accident path.
set -eu

SVC_USER="${1:-}"
if [ "$(id -u)" != 0 ]; then
    echo "run as root: sudo ./tools/setup.sh <service-user>"
    exit 1
fi
if [ -z "$SVC_USER" ]; then
    printf "service user the daemons run as [coder]: "
    read -r SVC_USER
    [ -z "$SVC_USER" ] && SVC_USER="coder"
fi
if ! id "$SVC_USER" >/dev/null 2>&1; then
    echo "user '$SVC_USER' does not exist , create it or pass a different one"
    exit 1
fi
REPO="$(cd "$(dirname "$0")/.." && pwd)"
ENVFILE=/etc/ghost/ghost.env
say() { printf '\n=== %s ===\n' "$1"; }
ask() { # ask <prompt> <default> ; echoes the answer
    _p="$1"; _d="${2:-}"
    if [ -n "$_d" ]; then printf '%s [%s]: ' "$_p" "$_d" >&2; else printf '%s: ' "$_p" >&2; fi
    read -r _a || true
    [ -z "$_a" ] && _a="$_d"
    printf '%s' "$_a"
}
confirm_word() { # confirm_word <word> ; true only if the user types <word> exactly
    printf 'type %s to proceed (anything else aborts): ' "$1" >&2
    read -r _c || true
    [ "$_c" = "$1" ]
}

# ---------------------------------------------------------------------------
say "1/6  Box prep (root)"
# server_setup_root.sh is idempotent; run it so packages/TPM/sudo/env are all in place. --host is
# only honoured when ghost.env does not yet exist, which is exactly right on a re-run.
CUR_HOST=""
[ -f "$ENVFILE" ] && CUR_HOST="$(grep '^GHOST_HOST=' "$ENVFILE" 2>/dev/null | head -1 | cut -d= -f2- || true)"
if [ -z "$CUR_HOST" ]; then
    CUR_HOST="$(ask "box host (LAN IP or FQDN the phone connects to)" "")"
    "$REPO/tools/server_setup_root.sh" --user "$SVC_USER" --host "$CUR_HOST" >/dev/null
else
    "$REPO/tools/server_setup_root.sh" --user "$SVC_USER" >/dev/null
fi
echo "  host: $CUR_HOST   user: $SVC_USER   repo: $REPO"

# ---------------------------------------------------------------------------
say "2/6  Verify as the service user"
sudo -u "$SVC_USER" sh -c "cd '$REPO' && ./tools/server_setup_user.sh" || {
    echo "  the user-side check reported problems above. Fix them, then re-run."
    exit 1
}

# ---------------------------------------------------------------------------
say "3/6  Build the binaries (as $SVC_USER)"
echo "  make box ..."
sudo -u "$SVC_USER" sh -c "cd '$REPO' && make box"
if [ ! -x "$REPO/bin/ghost-setup" ]; then
    echo "  build did not produce bin/ghost-setup , stopping."
    exit 1
fi
echo "  built: $REPO/bin/ghost-setup"

# ---------------------------------------------------------------------------
say "4/6  Choose the data disk"
echo "  Whole disks on this box (the data disk should be EMPTY , no partitions, not the OS disk):"
echo
lsblk -dpno NAME,SIZE,TYPE,MODEL 2>/dev/null | grep -E 'disk' | sed 's/^/    /'
echo
echo "  Partitions/mounts (for context , anything mounted here is NOT your target):"
lsblk -pno NAME,SIZE,MOUNTPOINT 2>/dev/null | grep -E '/' | sed 's/^/    /' || true
echo
DISK="$(ask "data disk to provision (e.g. /dev/nvme1n1)" "")"
if [ -z "$DISK" ] || [ ! -b "$DISK" ]; then
    echo "  '$DISK' is not a block device , aborting."
    exit 1
fi
# Refuse the obvious footgun: a disk with a mounted partition is almost certainly the OS/data-in-use.
if lsblk -pno NAME,MOUNTPOINT "$DISK" 2>/dev/null | awk 'NF>1{f=1} END{exit !f}'; then
    echo
    echo "  WARNING: $DISK has a MOUNTED partition. This is how you destroy the wrong disk."
    lsblk -pno NAME,SIZE,MOUNTPOINT "$DISK" | sed 's/^/    /'
    confirm_word "IUNDERSTAND" || { echo "  aborted."; exit 1; }
fi
echo "  selected: $DISK   (setup will DESTROY everything on it)"

# ---------------------------------------------------------------------------
say "5/6  Seal tier"
echo "  How the disk key is protected:"
echo "    tpm       hardware , key sealed in the fTPM (Intel PTT), hardware lockout on PIN guessing."
echo "              The real-box default. Requires a usable TPM 2.0."
echo "    software  PIN-derived , key wrapped under Argon2id(PIN) in seal.env. Genuinely encrypted,"
echo "              but NO hardware lockout: a stolen disk can be brute-forced offline. Dev/TPM-less."
echo
SEAL="$(ask "seal tier (tpm/software)" "tpm")"
case "$SEAL" in
    tpm|software) ;;
    *) echo "  '$SEAL' is not a tier , aborting."; exit 1;;
esac
DOMAIN="$(ask "public domain (blank for LAN-only)" "$CUR_HOST")"
DOMAIN_ARG=""
[ -n "$DOMAIN" ] && DOMAIN_ARG="--domain $DOMAIN"

# ---------------------------------------------------------------------------
say "6/6  Dry run, then apply"
COMMON="--user $SVC_USER --disk $DISK --host $CUR_HOST $DOMAIN_ARG --seal $SEAL"
echo "  ghost-setup $COMMON"
echo
echo "  DRY RUN (touches nothing):"
# shellcheck disable=SC2086
"$REPO/bin/ghost-setup" $COMMON || { echo "  dry run failed , not applying."; exit 1; }
echo
echo "  Review the plan above , especially the partition line for $DISK."
echo "  APPLYING will partition $DISK, mint the box CA, write nginx + units, start the daemons, and"
echo "  render the enrolment QR. It prompts for the main PIN and the wipe PIN on this terminal."
echo
if confirm_word "APPLY"; then
    # shellcheck disable=SC2086
    "$REPO/bin/ghost-setup" $COMMON --apply
    echo
    echo "=== Provisioned. Next: scan the QR above with the app (scanning IS enrolment), then unlock"
    echo "    with the main PIN. See tools/README.md steps 6-8 for models, the DB bundle, and the"
    echo "    first-unlock checks (including the PTT cold-power-cycle if the TPM is in lockout)."
else
    echo "  Not applied. Re-run tools/setup.sh when ready; nothing was changed."
fi