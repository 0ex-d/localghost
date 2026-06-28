# notify , the background poll

The phone asks the box every fifteen minutes whether anything happened, and shows it if so. That is the
whole job, and it is deliberately small, because the phone holds nothing, so a notification is just the
box saying "there is something waiting" (generic) or what it is (specific), depending on the person's
setting.

The poll is a tiny payload, so it runs unconstrained on a fifteen-minute WorkManager schedule. The
foreground poller covers the case where the app is open. MuteReceiver and NotifyState handle the mute,
which the person sets when they do not want to be pinged for a while.

This package is in for real change after today's server work, and it is the place a subtle deniability
property has to hold. The poller and the foreground now share one session token, and a wrong PIN on the
box revokes it, so both have to go dark together. If the poller kept showing notifications while the
foreground was presenting as down, that contradiction would give the whole appear-down trick away. So
when the box returns "down", this package has to treat it as genuinely down and clear its local cache,
not show stale notifications. The mute also grew on the box, per-service plus global with presets and a
custom duration, and the UI for that does not exist here yet. See the app status doc.
