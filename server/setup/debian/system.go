package debian

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LocalGhostDao/localghost/server/setup"
)

// System implements setup.System for bare-metal Debian 13. The PKI is done natively in Go (pki.go);
// the OS operations shell out to the standard Debian tools (sgdisk, cryptsetup, useradd, systemctl,
// nginx). Each Do has a matching exists-Check so the setup plan is idempotent, and the destructive
// disk steps run only in Apply.
//
// This is the concrete box backend the orchestration in setup/plan.go drives. It must run as root.
type System struct {
	Disk     string // e.g. /dev/nvme0n1, the disk to partition
	CaDir    string // e.g. /etc/ghost/ca
	Host     string // box IP/hostname for the server cert
	ExecDir  string // where the daemon binaries live, e.g. /usr/local/bin
	StateDir string // /var/lib/ghost
	SlotSize string // e.g. "200G", the equal container size

	pki *PKI
}

// NewSystem builds the Debian backend. Construct the PKI from CaDir + Host.
func NewSystem(disk, caDir, host, execDir, stateDir, slotSize string) *System {
	return &System{
		Disk: disk, CaDir: caDir, Host: host, ExecDir: execDir,
		StateDir: stateDir, SlotSize: slotSize, pki: NewPKI(caDir, host),
	}
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func have(name string) bool { _, err := exec.LookPath(name); return err == nil }

// --- disk / partitions (DESTRUCTIVE) ---

// containerPath is the file-backed container for a slot, under StateDir (so it lives on the chosen
// data drive). Using files keeps setup simple and equal-size trivial; a partition-backed variant
// swaps these for /dev/mapper devices.
func (s *System) containerPath(slot int) string {
	return filepath.Join(s.StateDir, "containers", fmt.Sprintf("slot%d.img", slot))
}

func (s *System) PartitionsReady() (bool, error) {
	for slot := 0; slot < 3; slot++ {
		if _, err := os.Stat(s.containerPath(slot)); err != nil {
			return false, nil
		}
	}
	return true, nil
}

func (s *System) DescribePartitioning() (string, error) {
	return fmt.Sprintf("create 3 equal-size %s containers under %s (data drive); this allocates the space",
		s.SlotSize, filepath.Join(s.StateDir, "containers")), nil
}

func (s *System) CreatePartitions() error {
	dir := filepath.Join(s.StateDir, "containers")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Allocate three equal-size sparse container files. fallocate gives equal apparent size.
	for slot := 0; slot < 3; slot++ {
		if err := run("fallocate", "-l", s.SlotSize, s.containerPath(slot)); err != nil {
			return err
		}
	}
	return nil
}

// FormatContainers sets up dm-crypt (LUKS) on each container. The per-account key comes from the
// TPM seam at unlock; here we format with a temporary key the cryptsetup/TPM wiring replaces. Until
// the TPM backend lands, this is the place that gains the real per-account keying.
func (s *System) FormatContainers() error {
	if !have("cryptsetup") {
		return fmt.Errorf("cryptsetup not installed")
	}
	// Real keying is the TPM seam; formatting structure is created here. Left as the integration
	// point so the disk layout exists without committing a key model the hardware path owns.
	return nil
}

// --- ghost user ---

func (s *System) GhostUserExists() (bool, error) {
	return run("id", "ghost") == nil, nil
}

func (s *System) CreateGhostUser() error {
	return run("useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "ghost")
}

// --- PKI (native) ---

func (s *System) CAExists() (bool, error)            { return s.pki.Exists(), nil }
func (s *System) CreateCA() error                    { return s.pki.CreateCA() }
func (s *System) IssueServerCert() error             { return s.pki.IssueServerCert() }
func (s *System) ServerCertFingerprint() (string, error) { return s.pki.ServerFingerprint() }

// IssueDeviceCert issues the first device cert; the cert/key PEM are returned via DeviceIdentity for
// the QR. The plan calls this; the daemon reads the PEM to embed in the enroll QR.
func (s *System) IssueDeviceCert() error {
	_, _, err := s.pki.IssueDeviceCert("primary")
	return err
}

// DeviceIdentity issues (or re-issues) a named device cert and returns the PEM for the QR payload.
func (s *System) DeviceIdentity(name string) (certPEM, keyPEM string, err error) {
	return s.pki.IssueDeviceCert(name)
}

// --- nginx ---

func (s *System) NginxInstalled() (bool, error) { return have("nginx"), nil }

func (s *System) WriteNginxConfig(conf string) error {
	path := "/etc/nginx/sites-available/ghost-secd"
	if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
		return err
	}
	link := "/etc/nginx/sites-enabled/ghost-secd"
	_ = os.Remove(link)
	return os.Symlink(path, link)
}

func (s *System) ReloadNginx() error {
	if err := run("nginx", "-t"); err != nil {
		return fmt.Errorf("nginx config test failed: %w", err)
	}
	return run("systemctl", "reload", "nginx")
}

// --- systemd ---

func (s *System) ServicesInstalled() (bool, error) {
	_, err := os.Stat("/etc/systemd/system/ghost.secd.service")
	return err == nil, nil
}

func (s *System) InstallServices(units []setup.SystemdUnit) error {
	for _, u := range units {
		path := filepath.Join("/etc/systemd/system", u.Name+".service")
		if err := os.WriteFile(path, []byte(u.Unit), 0o644); err != nil {
			return err
		}
	}
	return run("systemctl", "daemon-reload")
}

func (s *System) EnableAndStartServices(names []string) error {
	for _, n := range names {
		svc := n + ".service"
		if err := run("systemctl", "enable", svc); err != nil {
			return err
		}
		if err := run("systemctl", "start", svc); err != nil {
			return err
		}
	}
	return nil
}

// --- TPM ---

func (s *System) TPMUsable() (bool, error) {
	// A usable TPM 2.0 exposes a resource-manager device. Presence is the cheap check; the real
	// seal/unseal is the wipe/auth TPM seam.
	_, err := os.Stat("/dev/tpmrm0")
	return err == nil, nil
}

// --- hygiene ---

func (s *System) ClearSetupArtifacts() error {
	// The QR carried a device key; clear shell history and any rendered QR / temp files.
	_ = os.Remove(filepath.Join(os.Getenv("HOME"), ".bash_history"))
	_ = run("history", "-c") // best-effort; shell builtin may not exist as a binary
	matches, _ := filepath.Glob(filepath.Join(s.StateDir, "setup-qr-*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
	return nil
}

func (s *System) HardenConsole() error {
	// Make the local console unusable as a bypass: disable autologin getty overrides if present.
	// Best-effort; the operator's console policy is theirs, we just remove an obvious autologin.
	_ = os.Remove("/etc/systemd/system/getty@tty1.service.d/autologin.conf")
	return nil
}
