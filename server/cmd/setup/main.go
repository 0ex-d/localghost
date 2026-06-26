// ghost-setup provisions the box: it runs the setup plan (partition, CA, certs, nginx, systemd),
// then renders the enrolment QR and prints the one-time pairing code to start ghost.secd with.
//
// Flow:
//	ghost-setup --disk /dev/nvme0n1 --host 192.168.1.50 --plan      # dry run, shows what it will do
//	ghost-setup --disk /dev/nvme0n1 --host 192.168.1.50 --apply     # provisions, then prints QR+code
//
// After --apply it prints the exact command to launch the daemon with enrolment armed.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/LocalGhostDao/localghost/server/pair"
	"github.com/LocalGhostDao/localghost/server/setup"
	"github.com/LocalGhostDao/localghost/server/setup/debian"
)

func main() {
	disk := flag.String("disk", "", "disk to provision, e.g. /dev/nvme0n1 (DESTRUCTIVE)")
	host := flag.String("host", "", "box LAN IP/hostname the phone connects to")
	domain := flag.String("domain", "", "optional public domain (omit for the zero-server QR default)")
	caDir := flag.String("ca", "/etc/ghost/ca", "box CA + cert directory")
	execDir := flag.String("exec", "/usr/local/bin", "where the daemon binaries are installed")
	stateDir := flag.String("state", "/var/lib/ghost", "unencrypted state dir")
	slotSize := flag.String("slot-size", "200G", "equal size of each account container")
	port := flag.Int("port", 8443, "mTLS port ghost.secd serves behind nginx")
	apply := flag.Bool("apply", false, "actually provision (default is a dry run)")
	flag.Parse()

	if *host == "" {
		fmt.Fprintln(os.Stderr, "ghost-setup: --host is required")
		os.Exit(2)
	}

	sys := debian.NewSystem(*disk, *caDir, *host, *execDir, *stateDir, *slotSize)

	// nginx config + systemd units the plan installs.
	ghostSecdAddr := fmt.Sprintf("127.0.0.1:%d", *port)
	var nginxConf string
	withDomain := *domain != ""
	if withDomain {
		nginxConf = setup.DomainConfig{Domain: *domain}.NginxConfig(ghostSecdAddr)
	}
	units := setup.SystemdUnits(*execDir, setup.DaemonConfig{
		Host: *host, CaDir: *caDir, StateDir: *stateDir, Port: *port,
	})

	plan := setup.DefaultPlan(sys, withDomain, nil, nginxConf, units)

	planned, err := plan.DryRun()
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup precondition failed:", err)
		os.Exit(1)
	}
	fmt.Println("Setup plan:")
	for _, p := range planned {
		mark := " "
		if p.Destructive {
			mark = "!"
		}
		line := p.Action
		if p.Skip {
			line = "already satisfied , will skip"
		}
		if p.Problem != nil {
			line = "PRECONDITION: " + p.Problem.Error()
		}
		fmt.Printf("  [%s] %s , %s\n", mark, p.Name, line)
	}
	fmt.Println()

	if !*apply {
		fmt.Println("This was a dry run. Re-run with --apply to provision. Steps marked [!] are destructive.")
		return
	}

	results, err := plan.Apply(planned)
	for _, r := range results {
		status := "ok"
		switch r.Status {
		case setup.Failed:
			status = "FAILED"
			if r.Err != nil {
				status += ": " + r.Err.Error()
			}
		case setup.AlreadyDone:
			status = "skipped (already done)"
		}
		fmt.Printf("  %s , %s\n", r.Name, status)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nsetup stopped at the first failure above.")
		os.Exit(1)
	}

	// Provisioned. Render the enrolment QR and the one-time pairing code.
	fmt.Println("\nBox provisioned. Enrol your phone:")
	code, err := pair.Run(os.Stdout, pair.Options{
		Host:     *host,
		Port:     *port,
		CertPath: *caDir + "/box-server.pem",
		BoxName:  *host,
	}, pair.EncodeQR)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not render enrolment QR:", err)
		os.Exit(1)
	}

	// Write the one-time code where the systemd unit's EnvironmentFile picks it up, so the running
	// ghost.secd arms enrolment without the code ever living in the unit file. ghost.secd clears it
	// after a successful enrol.
	envPath := *stateDir + "/enroll.env"
	if err := os.WriteFile(envPath, []byte("GHOST_PAIRING_CODE="+code+"\n"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "could not write enrol env:", err)
		os.Exit(1)
	}

	// The plan already started ghost.secd, but it came up before this code existed. Restart it now so
	// it reads enroll.env and arms the first enrolment. We do this here rather than printing a manual
	// step, because a skipped restart silently breaks enrolment , the QR would scan but the box would
	// reject the code. If the restart fails (e.g. running setup without systemd), fall back to the
	// printed instruction.
	if out, rerr := exec.Command("systemctl", "restart", "ghost.secd").CombinedOutput(); rerr != nil {
		fmt.Println("\nEnrolment is armed, but the automatic restart failed:",
			strings.TrimSpace(string(out)))
		fmt.Println("Restart ghost.secd by hand so it picks up the code:")
		fmt.Println("  systemctl restart ghost.secd")
	} else {
		fmt.Println("\nEnrolment is armed and ghost.secd has picked up the code.")
	}
	fmt.Println("Scan the QR above with the LocalGhost app. The code is single use and clears after one enrol.")
}
