# internal/setup

Turning a bare box into a running one, in a way you can rehearse before you commit. The plan is a list of
ordered steps, each one knowing how to check whether it is already done, how to describe itself for a dry
run, and how to do the work. So you can run it twice safely, and you can see exactly what it intends to do
(the destructive disk steps are marked) before anything is touched.

System is an interface here, which is the trick that makes the orchestration testable with a fake while
the real thing shells out to the actual Debian tools. That lives in setup/debian.

The order matters and it is deliberate. The destructive disk work comes first and only on apply, then
the identity (the box is its own certificate authority, no Let's Encrypt and no port 80), then nginx,
the systemd services, the TPM check, and the cleanup that wipes the QR and key material setup leaves
behind. nginx is where two of the security properties get written, the mTLS gate that drops any client
without a box-issued device cert, and the appear-down handling that makes a wrong PIN look like a dead
upstream.

One chain in here has bitten us once and will bite again if it drifts. The disk path has to travel intact
from ghost-setup, through the systemd unit, to the ghost.secd flag, to the mounter that opens it. If
setup formats one disk and the daemon tries to open another, provisioning succeeds and every unlock
fails. The pieces are wired end to end now; keep them that way.

nginx config is only written when there is a domain. The QR-only LAN path writes none, so appear-down
applies on the domain path, which is the box's real setup.
