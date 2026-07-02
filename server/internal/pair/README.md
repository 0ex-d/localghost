# internal/pair

How a phone earns its way in for the first time, without any server in the loop. The box prints a QR at
setup. The QR carries the trust anchor (the fingerprint of the box's own server cert, which the phone
pins) and a one-time code. The phone scans it, enrols, and walks away with a device cert signed by the
box's CA. No Let's Encrypt, no third party, nothing on the public internet sees the exchange.

The point worth holding onto is that reachability is not access. A scanner that finds the box can reach
exactly one endpoint, enrolment, and that is gated by a one-time code that is rate-limited and single
use. Getting a packet to the box buys you nothing on its own.

The package carries the whole flow, the pairing state and one-time code, the enrol link format and its
parser, and the QR encoding plus the terminal rendering so setup can draw it on a console.
