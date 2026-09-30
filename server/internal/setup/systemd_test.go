package setup

import (
	"strings"
	"testing"
)

// secd's unit forbids core dumps (inherited by everything it starts) and keeps its hardening.
func TestSecdUnitHasNoCoreDumps(t *testing.T) {
	u := SystemdUnits("/opt/localghost/bin", DaemonConfig{StateDir: "/var/lib/ghost", Disk: "/dev/disk/by-id/x", Port: 8443, RunUser: "coder"})
	if len(u) != 1 {
		t.Fatalf("%d units", len(u))
	}
	for _, must := range []string{"LimitCORE=0", "ProtectHome=yes", "User=root", "--addr 127.0.0.1:8443"} {
		if !strings.Contains(u[0].Unit, must) {
			t.Fatalf("unit lacks %q:\n%s", must, u[0].Unit)
		}
	}
}
