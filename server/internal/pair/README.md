# internal/pair

How a phone earns its way in for the first time, without any server in the loop. The box prints a QR at
setup. The QR carries everything: the trust anchor (the fingerprint of the box's own server cert, which
the phone pins) AND the device identity itself , a client cert signed by the box's CA plus its private
key, both raw DER, base64url in the link. Scanning IS enrolment: the phone imports the identity and
walks away. No exchange, no pairing code, no enrolment endpoint, no Let's Encrypt, no third party,
nothing on the public internet sees anything.

The earlier design gated a network enrol endpoint with a one-time code. That is gone, and honestly why:
once the QR carries the cert, a code-for-cert exchange defends a door that no longer exists, and a code
nobody redeems is security theater. Removing the endpoint also removed the only certless route the edge
ever had , see setup/domain.go, where every request without a verified device cert now collapses to the
same 503 as a down box.

The trade this makes, stated plainly: the QR is a credential. It contains the device private key, shown
once on your own terminal over your own SSH session. Treat the screen accordingly and clear it after
the scan. The box never writes the key to disk (grep the CA dir: no device key files, by design), and a
new QR mints a fresh identity , that is the rotation story. What rotation does NOT do is revoke: there
is no CRL, so an old issued cert stays TLS-valid until the box CA itself is rotated; an old credential's
real obstacle is the second gate, the rate-limited PIN.

The package carries the whole flow: the enrol link format and its parser, device identity issuance via
the IssueDevice seam, and the QR encoding (byte mode, v1-20, level M with a level-L fallback for the
cert-bearing payload) plus the terminal rendering , half-block or quadrant, picked to fit the console ,
so setup can draw it.
