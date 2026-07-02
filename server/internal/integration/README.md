# internal/integration

Connectors (a bank, a calendar, an email account, cloud storage) belong to the account, not to the box.
Their tokens are sealed inside the account's encrypted store and decrypt only when the account is
mounted, so they vanish with everything else on a crypto-erase. Nothing about a connector is global.

A connector starts paused. It is configured but not polling, and turning it live takes an explicit
enable. That is not laziness, it is hygiene. A connector that quietly refreshes in the background is a
behaviour an observer could notice, so nothing reaches out until the person says so.

The decoy machinery that used to live here (decoys staying paused, enable being refused on a decoy) went
with the single-account move. What stayed is the paused-by-default rule, which earns its place on its
own without any of the decoy story around it.
