package setup

import (
	"fmt"
	"strings"
)

// The daemons that run on a box. ghost.secd is the front door; the rest bind loopback only and sit
// behind it. Setup renders a hardened systemd unit for each.
// GhostDaemons is the full roster of LocalGhost processes. Only ghost.secd gets a systemd unit
// (see SystemdUnits); the rest are supervised by ghost.secd on the encrypted volume. Kept here as
// the canonical list and referenced by the installer to know which daemon binaries to place.
var GhostDaemons = []string{
	"ghost.secd",    // front door / trust boundary (this service)
	"ghost.noted",   // notes
	"ghost.framed",  // image -> journal
	"ghost.voiced",  // voice
	"ghost.tallyd",  // tallies
	"ghost.synthd",  // synthesis
	"ghost.cued",    // cues
	"ghost.mistd",   // ...
	"ghost.shadowd", // ...
	"ghost.watchd",  // watch
}

// DaemonConfig is the runtime configuration the ghost.secd unit needs in its ExecStart. The backing
// daemons take no flags; only ghost.secd needs the box identity to issue device certs.
type DaemonConfig struct {
	Host     string // box IP/hostname for device cert issuance
	CaDir    string // /etc/ghost/ca
	StateDir string // /var/lib/ghost
	Disk     string // the raw LUKS data disk ghost.secd mounts on unlock, e.g. /dev/nvme1n1
	Port     int    // mTLS port behind nginx
}

// SystemdUnits renders a unit per daemon. ghost.secd is the only one that binds a public-facing
// socket (behind nginx); every other daemon is loopback-only and depends on ghost.secd. All run as
// the unprivileged ghost user with filesystem and capability hardening, so a compromised daemon has
// a small blast radius.
//
// DaemonConfig supplies ghost.secd's flags.
func SystemdUnits(execDir string, cfg DaemonConfig) []SystemdUnit {
	// Exactly ONE unit: ghost.secd. The ghost.*d daemons live on the encrypted volume and are
	// supervised by ghost.secd after unlock (internal/secd/supervisor.go), NOT by systemd , a boot
	// unit would try to start them against an unmounted volume and fail.
	return []SystemdUnit{
		{Name: "ghost.secd", Unit: renderUnit("ghost.secd", execDir, cfg)},
	}
}

func renderUnit(name, execDir string, cfg DaemonConfig) string {
	isSecd := name == "ghost.secd"
	var after, requires string
	if !isSecd {
		// Backing daemons start after and depend on the front door.
		after = "ghost.secd.service"
		requires = "ghost.secd.service"
	} else {
		after = "network-online.target"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[Unit]\n")
	fmt.Fprintf(&b, "Description=LocalGhost %s\n", name)
	fmt.Fprintf(&b, "After=%s\n", after)
	if requires != "" {
		fmt.Fprintf(&b, "Requires=%s\n", requires)
	}
	if isSecd {
		fmt.Fprintf(&b, "Wants=network-online.target\n")
	}

	fmt.Fprintf(&b, "\n[Service]\n")
	fmt.Fprintf(&b, "Type=notify\n")
	// ghost.secd's flags: box identity + state + the raw disk it mounts on unlock. No enrolment env
	// , the QR carries the device cert directly, so there is no pairing code or enroll.env. (This is
	// the only unit now; the ghost.*d daemons are supervised by ghost.secd, not systemd.)
	fmt.Fprintf(&b, "ExecStart=%s/%s --host %s --ca %s --state %s --disk %s --addr 127.0.0.1:%d\n",
		execDir, name, cfg.Host, cfg.CaDir, cfg.StateDir, cfg.Disk, cfg.Port)
	fmt.Fprintf(&b, "User=ghost\nGroup=ghost\n")
	fmt.Fprintf(&b, "Restart=on-failure\nRestartSec=2\n")
	// Hardening , a compromised daemon should not be able to roam.
	fmt.Fprintf(&b, "NoNewPrivileges=yes\n")
	fmt.Fprintf(&b, "ProtectSystem=strict\n")
	fmt.Fprintf(&b, "ProtectHome=yes\n")
	fmt.Fprintf(&b, "PrivateTmp=yes\n")
	// ghost.secd needs REAL device nodes (TPM, the raw disk, dm-crypt) to unseal and mount, so it
	// cannot have a private /dev. DeviceAllow below scopes what it may touch.
	fmt.Fprintf(&b, "PrivateDevices=no\n")
	fmt.Fprintf(&b, "ProtectKernelTunables=yes\n")
	fmt.Fprintf(&b, "ProtectControlGroups=yes\n")
	fmt.Fprintf(&b, "RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX\n")
	fmt.Fprintf(&b, "MemoryDenyWriteExecute=yes\n")
	fmt.Fprintf(&b, "LockPersonality=yes\n")
	// State dir under /var/lib/ghost, owned by the ghost user.
	fmt.Fprintf(&b, "StateDirectory=ghost\n")
	// ghost.secd needs TPM + the container devices; the others do not.
	if isSecd {
		fmt.Fprintf(&b, "DeviceAllow=/dev/tpmrm0 rw\n")
		fmt.Fprintf(&b, "SupplementaryGroups=tss disk\n")
	}

	fmt.Fprintf(&b, "\n[Install]\n")
	fmt.Fprintf(&b, "WantedBy=multi-user.target\n")
	return b.String()
}
