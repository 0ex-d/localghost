// ghost-update-guard runs before every start of ghost.secd (ExecStartPre, the unit's drop-in): when a
// new release is on trial and has not stayed up a minute three starts in a row, it puts the earlier
// build back before systemd starts ghost.secd again (internal/update). It never stops secd from
// starting: whatever happens, it exits 0.
//
// Kept out of releases on purpose: it is what undoes a bad one, so a release never replaces it.
// redeploy.sh and setup install it.
package main

import (
	"fmt"
	"os"

	"github.com/LocalGhostDao/localghost/server/internal/update"
)

func main() {
	rolled, err := update.Guard(update.BoxPaths())
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "ghost-update-guard: %v\n", err)
	case rolled:
		t := update.LoadTrial(update.BoxPaths())
		fmt.Fprintf(os.Stderr, "ghost-update-guard: rolled back from %s to %s: %s\n", t.Version, t.Prev, t.Reason)
	}
	os.Exit(0)
}
