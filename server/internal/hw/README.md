# internal/hw

This is where the careful logic in every other package meets the actual box, and where it will break
first. Everything that touches real hardware lives here. The TPM, the LUKS volume on the raw disk, and
the per-account Postgres and Redis that run inside it. None of it runs in CI. It shells out to
cryptsetup, initdb, pg_ctl, psql, redis-server, and talks to /dev/tpmrm0, so the only honest test is on
the box itself.

The reason to keep it in one place is blast radius. If a shell-out is wrong, or a TPM call behaves
differently than the docs claim, it fails here and not threaded through the rest of the codebase.

## The key model, which everything else assumes

The disk is one LUKS container over the whole raw device, locked by a random 32-byte key (the AMK). The
AMK is sealed in the TPM and bound to the main PIN. That gives the property the whole product rests on.

At setup, a random AMK is sealed under the PIN and the disk is formatted with it. At unlock, the PIN
unseals the AMK and the disk opens. The AMK is never derived from the PIN, so a short PIN does not mean a
weak key, and the AMK never leaves a TPM unseal, so a stolen powered-off disk cannot be attacked offline.
The TPM rate-limits wrong PINs in hardware. A wipe evicts the sealed AMK, the keyslot derived from it
goes useless, and 32 random bytes are not coming back.

One detail in here is load-bearing and looks like a typo if you do not know the story. Every cryptsetup
key feed passes `--keyfile-size 32`. The AMK is random binary and can contain a newline byte, which
cryptsetup would otherwise read as end-of-key and silently truncate, keying the disk with a short key
that fails unpredictably later. That flag is the fix. Leave it.

## The per-account data

The mute store and the notification store both run against the Postgres and Redis that live inside the
encrypted volume, so their contents are encrypted at rest and gone on crypto-erase. They shell out to
psql and redis-cli rather than pulling a Go database driver, which keeps the dependency list short and
matches how the rest of this package talks to its tools. Notifications are always produced and stored;
mute only changes what gets pushed, never what gets kept.

## Honest state

Nothing here has executed. The TPM round-trip, the disk format and mount, the database startup, all of
it is structurally verified and waiting on the box. The SQL is built by string-formatting with quote
escaping, which is fine while the only inputs are trusted in-volume daemons and an allow-listed scope,
and which should move to parameterised queries the day anything less trusted can reach it.
