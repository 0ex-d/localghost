// ghostctl is a command-line LocalGhost client. Its first job is enrolment: given an enroll link
// (the same string the box's QR carries), it saves the device identity the link itself delivers.
// The link IS the credential , the box generated the device cert + key and put them in the link
// (raw DER, base64url), so enrolment is local: parse, save, done. No network call, no pairing-code
// exchange; a session token is issued later, at first PIN unlock, exactly as on the phone. It is
// the test client for ghost.secd before the phone is wired.
//
//	ghostctl enroll "localghost://enroll?v=1&host=...&port=...&fp=...&name=...&cert=...&key=..."
package main

import (
	"encoding/pem"
	"fmt"
	"os"

	"github.com/LocalGhostDao/localghost/server/internal/pair"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "enroll" {
		fmt.Fprintln(os.Stderr, "usage: ghostctl enroll <enroll-link>")
		os.Exit(2)
	}
	link, err := pair.Parse(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad link:", err)
		os.Exit(1)
	}

	fmt.Printf("box      %s:%d\n", link.Host, link.Port)
	fmt.Printf("identity %s\n", link.Fingerprint)

	if err := enroll(link); err != nil {
		fmt.Fprintln(os.Stderr, "enrol failed:", err)
		os.Exit(1)
	}
}

// enroll saves the device identity carried inside the link. Mirrors the app's rule: cert/key are
// optional at parse time but REQUIRED to enrol, so a code-only link fails here with instructions
// rather than half-enrolling. Files are PEM (openssl-inspectable) even though the link carries DER.
func enroll(link pair.EnrollLink) error {
	if len(link.DeviceCertDER) == 0 || len(link.DeviceKeyDER) == 0 {
		return fmt.Errorf("this link carries no device certificate , regenerate the enrolment QR on the box")
	}
	dir := "./ghostctl-identity"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: link.DeviceCertDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: link.DeviceKeyDER})
	if err := os.WriteFile(dir+"/device.pem", certPEM, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(dir+"/device-key.pem", keyPEM, 0o600); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("enrolled. device identity saved to", dir)
	fmt.Println("a session token is issued at first PIN unlock, not at enrolment")
	return nil
}
