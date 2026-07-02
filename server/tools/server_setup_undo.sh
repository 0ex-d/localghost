#!/usr/bin/env bash
# Undo what server_setup_root.sh added. Run as root. Removes the config/group/symlinks/udev rule it
# created, and removes the service user from tss. Does NOT uninstall packages (cryptsetup, tpm2-tools
# are harmless + wanted) and does NOT unload dm_crypt (inert). Does NOT touch nginx or any disk ,
# the setup script never touched those either. Safe to run; only removes LocalGhost's own additions.
set -uo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }
SVC_USER="${1:-coder}"

echo "> removing scoped sudoers..."
rm -f /etc/sudoers.d/localghost-services && echo "  removed /etc/sudoers.d/localghost-services"

echo "> removing box env + state..."
rm -rf /etc/ghost && echo "  removed /etc/ghost"
rm -rf /var/lib/ghost && echo "  removed /var/lib/ghost"

echo "> removing postgres PATH symlinks..."
for b in initdb pg_ctl postgres; do
    [ -L "/usr/local/bin/$b" ] && rm -f "/usr/local/bin/$b" && echo "  removed /usr/local/bin/$b"
done

echo "> removing TPM udev rule..."
rm -f /etc/udev/rules.d/60-localghost-tpm.rules && echo "  removed udev rule"
udevadm control --reload >/dev/null 2>&1 || true

echo "> removing $SVC_USER from tss + deleting tss group..."
gpasswd -d "$SVC_USER" tss 2>/dev/null && echo "  removed $SVC_USER from tss" || true
groupdel tss 2>/dev/null && echo "  deleted tss group" || echo "  (tss group not removed , may be in use)"

echo "> removing dm_crypt boot-load entry (module stays loaded, harmless)..."
rm -f /etc/modules-load.d/localghost.conf && echo "  removed modules-load entry"

echo
echo "Undone. NOTE: packages (cryptsetup, tpm2-tools) left installed , they're harmless and wanted."
echo "If you really want them gone: apt-get remove cryptsetup tpm2-tools"
echo "The TPM device /dev/tpmrm0 group/mode will reset to root-only on next reboot (udev rule gone)."
