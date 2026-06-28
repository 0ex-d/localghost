# net , the box boundary

Everything the phone says to the box goes through here, and the whole package is built on one idea. The
phone is a limb, not a brain. It holds no data of its own, it authenticates to the box over a pinned
mutual-TLS channel, and it forgets everything the moment it locks. So this package is mostly about
proving identity in both directions and carrying small requests, not about storing or computing
anything.

The trust model is the part worth understanding first. The box is its own certificate authority. It
does not have a Let's Encrypt cert and the phone does not trust the public CA store. Instead the box
sends a self-signed cert, and the phone trusts exactly the one whose fingerprint it pinned at
enrolment. The other half of the handshake is the phone proving itself with a device cert the box
issued. Both halves have to hold or the connection does not happen, which is what keeps a stranger on
the same network from reaching anything.

BoxClient is the single seam the rest of the app talks through, so when the box contract changes, it
changes here and almost nowhere else. BoxHttp is the transport, plain HttpsURLConnection with no
third-party HTTP library, because the app keeps its dependencies near zero. BoxTrust does the pinning
and sends the device cert. DeviceCert holds the client certificate and key the box delivered through
the QR, kept in the encrypted store because the QR that carried it was a credential. EnrollLink is the
self-contained descriptor in that QR, everything the phone needs to find the box and pin it on first
contact, with no server in the loop. UnlockStream mirrors ghost.secd's unlock stages so the screen can
fill in as the box works through them.

This is the package that today's server changes land on hardest. Session tokens, appear-down, and the
shared-fate poller all change what BoxClient sends and how it reads a "down" response, and the app has
not caught up yet. See the app status doc.
