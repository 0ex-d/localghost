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
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8443", "listen address (behind nginx, which terminates public TLS)")
	stateDir := flag.String("state", "/var/lib/ghost", "unencrypted state dir (certs, models)")
	disk := flag.String("disk", os.Getenv("GHOST_DISK"), "the raw LUKS data disk to mount on unlock (e.g. /dev/nvme1n1); defaults to $GHOST_DISK")
	flag.Parse()

	// No enrolment wiring, no pairing code, no PKI hookup: the daemon never issues device certs.
	// Enrolment happens entirely at QR render time (ghost-setup -> pair.Run -> PKI), and the QR
	// itself carries the identity. The daemon only ever sees already-enrolled devices.
	srv, err := secd.New(secd.Config{StateDir: *stateDir, Disk: *disk})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghost.secd: init failed:", err)
		os.Exit(1)
	}

	log.Printf("ghost.secd listening on %s (state %s)", *addr, *stateDir)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}

