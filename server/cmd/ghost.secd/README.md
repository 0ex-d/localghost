# cmd/ghost.secd

The daemon. This is the binary that runs on the box as the front door, wiring the library packages into
one process behind nginx. It takes the box identity and the data disk on flags (--host, --ca, --state,
--addr, and --disk, which defaults to $GHOST_DISK and is how it knows which raw disk to open on unlock),
and it reads a one-time pairing code from the environment so the first enrolment can be armed without
baking a secret into the unit file.

The interesting code is all in internal/secd. This is the thin main that constructs it and listens.
