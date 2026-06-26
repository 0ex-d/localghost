// ghostctl is a command-line LocalGhost client. Its first job is enrolment: given an enroll link
// (the same string the box's QR carries), it connects to the box, pins the box certificate against
// the fingerprint in the link, completes the pairing-code exchange, and saves the device cert + key
// it receives. It is the test client for ghost.secd before the phone is wired.
//
//	ghostctl enroll "localghost://enroll?host=...&port=...&code=...&fp=...&name=..."
package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

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
	fmt.Printf("code     %s\n", link.Code)

	if err := enroll(link); err != nil {
		fmt.Fprintln(os.Stderr, "enrol failed:", err)
		os.Exit(1)
	}
}

func enroll(link pair.EnrollLink) error {
	// Pin the box server cert by the fingerprint from the link. We accept the box's self-signed cert
	// ONLY if its SHA-256 matches; the system trust store is irrelevant (the box is its own CA).
	want := normaliseFP(link.Fingerprint)
	tlsCfg := &tls.Config{
		InsecureSkipVerify: true, // we verify by pin below, not via the system roots
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no server certificate")
			}
			sum := sha256.Sum256(rawCerts[0])
			got := colonHex(sum[:])
			if normaliseFP(got) != want {
				return fmt.Errorf("certificate pin mismatch: box presented %s, expected %s", got, want)
			}
			return nil
		},
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}

	body, _ := json.Marshal(map[string]string{"pairingCode": link.Code, "deviceName": "ghostctl"})
	url := fmt.Sprintf("https://%s:%d/v1/enroll", link.Host, link.Port)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var out struct {
		OK            bool   `json:"ok"`
		DeviceToken   string `json:"deviceToken"`
		DeviceCertPem string `json:"deviceCertPem"`
		DeviceKeyPem  string `json:"deviceKeyPem"`
		Error         string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("bad response: %s", strings.TrimSpace(string(raw)))
	}
	if !out.OK {
		return fmt.Errorf("box refused: %s", out.Error)
	}

	// Save the device identity so later authenticated calls present it for mTLS.
	dir := "./ghostctl-identity"
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(dir+"/device.pem", []byte(out.DeviceCertPem), 0o600)
	_ = os.WriteFile(dir+"/device-key.pem", []byte(out.DeviceKeyPem), 0o600)
	_ = os.WriteFile(dir+"/token", []byte(out.DeviceToken), 0o600)

	fmt.Println()
	fmt.Println("enrolled. device identity saved to", dir)
	fmt.Println("token   ", out.DeviceToken)
	return nil
}

func normaliseFP(s string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(s, ":", ""), " ", ""))
}

func colonHex(b []byte) string {
	const hex = "0123456789ABCDEF"
	out := make([]byte, 0, len(b)*3)
	for i, x := range b {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hex[x>>4], hex[x&0x0f])
	}
	return string(out)
}
