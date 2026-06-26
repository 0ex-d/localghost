// ghost.secd is the box daemon: the single front door the phone connects to. It terminates the
// authenticated channel, runs enrolment, unlock, and serves info + the model catalogue, wiring the
// library packages (auth, profile, container, wipe, gateway, integration, models, pair) into one
// running process. The backing ghost.<x>d daemons sit on loopback behind it.
//
// This is the minimal server needed to: run setup, scan the QR, enrol a device, unlock, and pull
// info. The TPM/dm-crypt/per-account-DB seams are still stubbed where the hardware is needed; the
// HTTP surface and the flow are real so the app can connect end to end.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/LocalGhostDao/localghost/server/internal/secd"
	"github.com/LocalGhostDao/localghost/server/internal/setup/debian"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8443", "listen address (behind nginx, which terminates public TLS)")
	stateDir := flag.String("state", "/var/lib/ghost", "unencrypted state dir (certs, models, enrollment)")
	caDir := flag.String("ca", "/etc/ghost/ca", "box CA + cert directory")
	host := flag.String("host", "", "box host/IP the server cert is valid for (for issuing device certs)")
	flag.Parse()

	// The one-time pairing code comes from the environment (GHOST_PAIRING_CODE), not a flag, so that
	// systemd's EnvironmentFile can supply it for the first enrolment and leave it unset afterwards
	// without any dangling argument. Empty means "not armed".
	pairingCode := os.Getenv("GHOST_PAIRING_CODE")

	srv, err := secd.New(secd.Config{StateDir: *stateDir})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghost.secd: init failed:", err)
		os.Exit(1)
	}

	// Wire the box PKI so enrolment can mint device certs. The CA must already exist (setup creates
	// it); the PKI just signs device certs against it.
	pki := debian.NewPKI(*caDir, *host)
	srv.SetDeviceIssuer(pkiIssuer{pki})

	// Arm enrolment if setup handed us a one-time pairing code.
	if pairingCode != "" {
		srv.ArmEnrollment(pairingCode)
		log.Printf("enrolment armed with a one-time pairing code")
	}

	log.Printf("ghost.secd listening on %s (state %s)", *addr, *stateDir)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}

// pkiIssuer adapts the PKI to the server's DeviceIssuer interface.
type pkiIssuer struct{ pki *debian.PKI }

func (p pkiIssuer) DeviceIdentity(name string) (certPEM, keyPEM string, err error) {
	return p.pki.IssueDeviceCert(name)
}
