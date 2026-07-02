# internal/admin

Some operations are too dangerous to ever reach from the phone. PIN rotation, which destroys a key, is
the obvious one. So this package holds the operations that only work from the box itself, over a local
network session, and it fails closed if it cannot prove the session is local. A coerced phone cannot
trigger any of it, because the phone is never on the right side of the gate.

The local gate reads the session's real peer address and refuses unless it is provably loopback or a
private range. Anything public, ambiguous, or unparseable is rejected rather than waved through, because
the safe default for "I am not sure where this came from" is no.

PIN rotation is built as destroy-then-recreate, not rotate-in-place. Resetting a PIN is the same as
wiping that account and making a fresh one keyed by the new PIN. The ordering is the safety. The old key
is destroyed only after the new PIN has been entered and confirmed, so a typo at the new-PIN step aborts
and leaves the account untouched. A slip never costs you the volume.

One caveat to set expectations. This is simplified to the single account now (the decoy and duress slots
are gone), and nothing runs it yet. It is a standalone package waiting for a box console command to call
it, with no backend behind it, so do not assume anything is invoking it today.
