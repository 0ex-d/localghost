package secd

// secd-side helpers for the services running on the encrypted volume. The config TYPE and its
// read/write live in internal/hw (hw.ServicesConfig) so both DataStore (pg/redis) and this supervisor
// read one canonical file , services.conf on the mount, the single operational description of the
// box. This file keeps only the supervisor's policy: which daemons are critical, and where the
// binaries live.

import (
	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

// supervisedDaemons reads the daemon health-port map from services.conf on the mount. On a
// provisioned box the file exists (provision wrote it); if it cannot be read, the supervisor gets an
// empty map and starts nothing, which surfaces as the box serving with no daemons rather than
// guessing ports. Postgres/Redis are handled by DataStore, not listed here.
func supervisedDaemons(mountPath string) map[string]int {
	c, err := hw.LoadServicesConfig(mountPath)
	if err != nil {
		return nil
	}
	return c.Daemons
}

// criticalServices are the daemons whose failure means the box serves with that capability erroring
// (but stays mounted, does not go dark). Postgres + Redis criticality is owned by DataStore. shadowd
// is critical because the deniability/answer path depends on it.
var criticalServices = map[string]bool{
	"ghost.shadowd": true,
}

// daemonBinDir is where the ghost.*d binaries are installed (same place as ghost.secd). The
// supervisor execs <daemonBinDir>/<name>.
const daemonBinDir = "/usr/local/bin"
