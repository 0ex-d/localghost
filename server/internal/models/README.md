# internal/models

Models are public weights, not personal data, so they live in the open part of the box and are shared
rather than copied per account. A per-account copy would buy nothing (the weights are the same public
files for everyone) and would cost several copies of multi-gigabyte downloads. So the catalogue is
unencrypted, sits outside any account container, and ghost.secd serves it from there.

Nothing account-specific belongs in this package. If a thing is private, it goes in the encrypted
volume, not here.
