// ghost-notifgen seeds believable notifications into the mounted account, for development. It exists
// so the notification surface (list, mute filtering, per-device cursor, seen, delete) has real-looking
// data to show while the actual daemons are still stubs. Templates, not a model , the box does not run
// inference and seed data does not need one. Remove it once the daemons produce for real.
//
// The account must be UNLOCKED (volume mounted, Postgres + Redis up) when this runs, since it writes
// into the in-volume databases. It does not unlock anything itself.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

func main() {
	stateDir := flag.String("state", "/var/lib/ghost", "unencrypted state dir (to locate the mount)")
	slot := flag.Int("slot", 0, "account slot (single-account model: 0)")
	n := flag.Int("n", 5, "how many notifications to seed")
	service := flag.String("service", "", "seed only this service (e.g. ghost.synthd); empty = random services")
	flag.Parse()

	// The mount path matches DMCryptMounter: <stateDir>/mnt/slot<N>, with the pg socket in its
	// "postgres" subdir. NotifStore shells out to psql/redis-cli against the per-slot ports.
	store := hw.NewNotifStore(func(s int) string {
		mnt := filepath.Join(*stateDir, "mnt", fmt.Sprintf("slot%d", s))
		return hw.SocketForMount(mnt)
	})
	gen := hw.NewNotifGen(store)

	var (
		stored int
		err    error
	)
	if *service != "" {
		stored, err = gen.SeedService(*slot, *service, *n)
	} else {
		stored, err = gen.Seed(*slot, *n)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "seeded %d before error: %v\n", stored, err)
		os.Exit(1)
	}
	fmt.Printf("seeded %d notifications into slot %d\n", stored, *slot)
}
