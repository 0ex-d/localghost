#!/usr/bin/env bash
# own_user.sh , give the daemons a user of their own, so nothing else on this machine can read the vault.
#
#   sudo ./tools/own_user.sh            the user is called ghostd
#   sudo ./tools/own_user.sh <name>     another name ([a-z_][a-z0-9_]*, not ghost, ghost_ro or ghost_rw)
#
# WHY. The decrypted volume lives in ghost.secd's private mount namespace, and the host cannot see
# it. A process of the SAME user as the daemons can: /proc/<pid>/root of any daemon (Postgres,
# Redis, llama-server, whisper-cli) is a door into that namespace, open to its own user. A box set
# up with the daemons under a login account (coder on xyntai) therefore lets anything else running
# as that account (a shell, an editor, a compromised website) read the whole vault. A system user
# nobody logs in as closes the door: only root and the daemons themselves are left.
#
# WHAT IT DOES. Lock the box from the app first (SETTINGS › LOCK BOX NOW); this stops if the volume
# is open.
#   1. makes the system user (no home, no shell, no password) if it is not there;
#   2. hands it what the daemons write outside the volume (/var/lib/ghost/backup, when present);
#   3. points ghost.secd's unit at it (--user), keeping the old unit beside it, and restarts secd.
# The next unlock from the phone hands the volume over, once (ghost.secd, internal/hw/adopt.go): a
# Postgres superuser of the new name, then every file the old user owned, chowned. A large archive
# takes a few seconds more on that one unlock; every unlock after is as before. The old account
# keeps building and deploying (redeploy.sh still builds as it) and loses nothing but the door.
#
# UNDO. The old unit is kept as ghost.secd.service.before-own-user: copy it back, daemon-reload,
# restart, and lock/unlock. The volume is handed back to the old user the same way.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then echo "run as root (sudo)" >&2; exit 1; fi
NEW="${1:-ghostd}"
UNIT=/etc/systemd/system/ghost.secd.service
MOUNT="${GHOST_MOUNT:-/var/lib/ghost/mnt/slot0}"

case "$NEW" in
    ghost|ghost_ro|ghost_rw|root) echo "'$NEW' is taken (a database role, or root); pick another name" >&2; exit 2 ;;
esac
if ! printf '%s' "$NEW" | grep -Eq '^[a-z_][a-z0-9_]*$'; then
    echo "'$NEW': a user name for this is [a-z_][a-z0-9_]* (it is also a Postgres role name)" >&2; exit 2
fi
[ -f "$UNIT" ] || { echo "no $UNIT: is LocalGhost installed on this machine?" >&2; exit 1; }

OLD="$(sed -n 's/^ExecStart=.* --user \([^ ]*\).*/\1/p' "$UNIT" | head -1)"
if [ "$OLD" = "$NEW" ]; then
    echo "the daemons already run as $NEW; nothing to do"
    exit 0
fi

PID="$(pidof ghost.secd || true)"; PID="${PID%% *}"
if [ -n "$PID" ] && [ -d "/proc/$PID/root$MOUNT/run" ]; then
    echo "the box is UNLOCKED: lock it from the app first (SETTINGS › LOCK BOX NOW), then run this again" >&2
    echo "(the volume is handed over at the next unlock, never under running daemons)" >&2
    exit 1
fi

# 1. the user
if id "$NEW" >/dev/null 2>&1; then
    uid="$(id -u "$NEW")"
    shell="$(getent passwd "$NEW" | cut -d: -f7)"
    case "$shell" in
        */nologin|*/false) ;;
        *) echo "$NEW exists and has a login shell ($shell); the point is a user nobody logs in as. Pick another name." >&2; exit 1 ;;
    esac
    if [ "$uid" -ge 1000 ]; then
        echo "$NEW exists as an ordinary account (uid $uid); pick another name" >&2; exit 1
    fi
    echo "user $NEW exists (uid $uid, no login): using it"
else
    useradd --system --user-group --no-create-home --home-dir /nonexistent \
        --shell /usr/sbin/nologin --comment "LocalGhost daemons" "$NEW"
    passwd -l "$NEW" >/dev/null 2>&1 || true
    echo "made system user $NEW (uid $(id -u "$NEW"), no home, no shell, no password)"
fi

# 2. what the daemons write outside the volume
for d in /var/lib/ghost/backup; do
    [ -d "$d" ] || continue
    if [ -n "$OLD" ] && [ "$(stat -c %U "$d")" = "$OLD" ]; then
        chown -R "$NEW:$NEW" "$d"
        echo "handed $d to $NEW"
    fi
done

# 3. the unit
cp -p "$UNIT" "$UNIT.before-own-user"
if grep -q '^ExecStart=.* --user ' "$UNIT"; then
    sed -i "/^ExecStart=/ s/ --user [^ ]*/ --user $NEW/" "$UNIT"
else
    sed -i "/^ExecStart=/ s/\$/ --user $NEW/" "$UNIT"
fi
grep -q "^ExecStart=.* --user $NEW\b" "$UNIT" || { echo "could not set --user in $UNIT; the old unit is at $UNIT.before-own-user" >&2; exit 1; }
systemctl daemon-reload
systemctl restart ghost.secd
echo "ghost.secd now runs the daemons as $NEW${OLD:+ (was $OLD)}; the old unit is at $UNIT.before-own-user"
echo
echo "next: unlock from the phone. That unlock hands the volume to $NEW (once; journalctl -u ghost.secd"
echo "shows \"volume handed to its own user\" with the count), and from then on nothing running as"
echo "${OLD:-another user} can read the vault. Check it with: sudo ./tools/privacy_check.sh"
