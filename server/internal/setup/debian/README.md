# internal/setup/debian

The real backend, for bare-metal Debian 13. The PKI is done natively in Go; everything else shells out
to the standard tools (cryptsetup, initdb and pg_ctl and psql, useradd, systemctl, nginx). It has to run
as root, and none of the disk or TPM work runs in CI, so this is one of the places the box gets the
final say.

This is where the keying model from hw becomes real commands. A random 32-byte AMK is generated, sealed
in the TPM under the main PIN, and the raw disk is formatted with that key. The PIN is chosen during
setup, which is what lets the seal and the format happen in a single pass without a chicken-and-egg
problem. Wrong PINs later will trip the TPM's own lockout, and a wipe evicts the sealed key and takes the
disk with it.

The TPM-sealing code has to stay behind the `tpm` build tag, and it does, in seal_tpm.go, with a sim
stub that refuses to provision without a TPM. The reason is mechanical. system.go itself is tag-neutral
so the simulation build can compile it, and if you put the TPM call directly in system.go the sim build
breaks. The pattern is the same one the rest of the codebase uses for hardware.

Two small commands in here are load-bearing and look optional until they are not. cryptsetup gets
`--keyfile-size 32` so a random key with a newline byte is not truncated, and mkfs runs with `-F` so it
does not stop on an interactive "a filesystem exists, proceed?" prompt during a re-provision and hang
forever with no one to answer.

Re-provisioning an already-formatted disk is refused, on purpose. Running setup again with a new PIN
would silently keep the original, because the format and seal steps skip when the disk is already LUKS.
Rather than quietly lock you out, ghost-setup stops and tells you to wipe the disk first if you really
mean to start over.
