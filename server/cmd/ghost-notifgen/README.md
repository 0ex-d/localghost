# cmd/ghost-notifgen

A development tool that seeds believable notifications into the mounted account, so the notification
surface (the list, the mute filtering, the per-device cursor, seen, delete) has real-looking data to
show while the actual daemons are still stubs.

It is templates, not a model. The box does not run inference (it serves weights for the phone to run),
and seed data does not need a 12B model anyway. A handful of per-service templates with randomised
fields gives variety with no dependency and no latency.

The account has to be unlocked when this runs, because it writes into the in-volume Postgres and Redis.
It does not unlock anything itself. Seed random services with `-n`, or one service with `-service
ghost.synthd` (handy for testing per-service mute). Everything goes through NotifStore.Produce, so it
exercises the real path, not a shortcut.

This goes away when the real daemons produce notifications for themselves.
