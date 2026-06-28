# sync , photos to the box

The one place the phone pushes something heavy to the box, the camera roll. Everything else is the phone
asking the box for small things; this is the phone handing over photos so they live on the box with the
rest of your life, not in someone else's cloud.

The fifteen-minute background sync respects a network setting, Wi-Fi only by default so it does not eat
mobile data, any network if the person opts in. Because the constraint is fixed when the job is
scheduled, the schedule has to be rebuilt when the toggle flips, which is the kind of detail that is
easy to miss and annoying to debug.

This package is also the app-side half of the deniability story. The design is that the phone keeps only
recent data, on the order of a week, and evicts the rest to the box. A seized phone then holds very
little, and the deniability comes from absence rather than from any decoy. That eviction and retention
window is the thing to confirm is enforced, because the whole deniability-by-absence claim
rests on it.
