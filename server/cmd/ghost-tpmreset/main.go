//go:build tpm

// ghost-tpmreset resets the TPM lockout hierarchy auth back to empty using the known pinAuth(PIN),
// so a stalled provisioning run can proceed. Needed when SetupLockout was run more than once with
// differing PINs and drove the lockout hierarchy into DA lockout, and the platform tpm2_clear build
// cannot pass a lockout auth on the CLI. Not part of normal operation , a repair tool.
//
//	ghost-tpmreset --tpm /dev/tpmrm0        # prompts for the PIN that owns the lockout auth
//
// After it succeeds, re-run ghost-setup --apply; SetupLockout starts from an empty lockout auth.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
	"golang.org/x/term"
)

func main() {
	device := flag.String("tpm", "/dev/tpmrm0", "TPM resource-manager device")
	flag.Parse()

	fmt.Print("PIN that owns the lockout auth (the one from your first successful provisioning run): ")
	var pin string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			fmt.Fprintln(os.Stderr, "read pin:", err)
			os.Exit(1)
		}
		pin = string(b)
	} else {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		pin = strings.TrimRight(line, "\r\n")
	}
	if pin == "" {
		fmt.Fprintln(os.Stderr, "empty PIN , aborting")
		os.Exit(2)
	}

	if err := hw.ResetLockoutAuth(*device, pin); err != nil {
		fmt.Fprintln(os.Stderr, "reset failed:", err)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "If the error mentions lockout/0x921: the TPM is still in DA lockout.")
		fmt.Fprintln(os.Stderr, "Recovery is 24h and every failed attempt re-arms it , stop, wait a full")
		fmt.Fprintln(os.Stderr, "day untouched, then retry ONCE.")
		fmt.Fprintln(os.Stderr, "If it mentions auth failure (not lockout): this PIN is not the one that set")
		fmt.Fprintln(os.Stderr, "the lockout auth. Try the other PIN, or `tpm2_clear -c l` if the auth is empty.")
		os.Exit(1)
	}
	fmt.Println("lockout auth reset to empty. Re-run: ghost-setup --apply")
}
