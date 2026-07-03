# cmd/ghost.secd

The daemon. This is the binary that runs on the box as the front door, wiring the library packages into
one process behind nginx. It takes the state dir and the data disk on flags (--state, --addr, and
--disk, which defaults to $GHOST_DISK and is how it knows which raw disk to open on unlock). It has no
enrolment wiring at all: no pairing code, no PKI hookup, no /v1/enroll. The QR rendered by ghost-setup
carries the device identity itself, so the daemon only ever hears from already-enrolled devices.

The interesting code is all in internal/secd. This is the thin main that constructs it and listens.
