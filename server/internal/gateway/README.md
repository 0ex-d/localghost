# internal/gateway

One chokepoint, so the security-relevant decisions are not scattered across the codebase. Auth, picking
the mounted account, and routing a request to the backing daemons all happen in one place. When the
decision about who gets to talk to what lives in a single package, it is reviewable. When it is sprinkled
through twenty handlers, it is not.

In the single-account model there is one mounted account and the gateway routes to its loopback daemons.
The old "a mounted decoy also routes" behaviour is gone, because there are no decoys to route for.
