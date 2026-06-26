package pair

import (
	"fmt"
	"io"
)

// Options for one pairing render. The daemon fills these from config/flags.
type Options struct {
	Host     string // LAN address or .local; empty -> auto-detect
	Port     int    // mTLS port the box serves on
	CertPath string // PEM cert served on that port; its SHA-256 is the trust anchor
	BoxName  string // human label (defaults to hostname elsewhere)
}

// Run mints a pairing code, builds the enroll link, and writes both the link text and a scannable
// terminal QR to w. Returns the code so the caller can register it as live (single-use, expiring)
// in the daemon's enrollment state.
//
// EncodeQR is the seam: it turns the link string into a Matrix. See qrencode.go, the
// from-scratch byte-mode encoder in qrencode.go (no third-party QR code).
func Run(w io.Writer, opts Options, encodeQR func(string) (Matrix, error)) (code string, err error) {
	host := opts.Host
	if host == "" {
		if host, err = LANHost(); err != nil {
			return "", err
		}
	}
	fp, err := CertFingerprint(opts.CertPath)
	if err != nil {
		return "", fmt.Errorf("reading cert fingerprint: %w", err)
	}
	code, err = NewPairingCode()
	if err != nil {
		return "", err
	}

	link := EnrollLink{
		Host:        host,
		Port:        opts.Port,
		Code:        code,
		Fingerprint: fp,
		BoxName:     opts.BoxName,
	}
	matrix, err := encodeQR(link.String())
	if err != nil {
		return "", fmt.Errorf("encoding QR: %w", err)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, RenderTerminal(matrix))
	fmt.Fprintln(w, "Scan this with the LocalGhost app, or enter the details by hand:")
	fmt.Fprintf(w, "  box     %s:%d\n", host, opts.Port)
	fmt.Fprintf(w, "  code    %s\n", code)
	fmt.Fprintf(w, "  finger  %s\n", fp)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Link:", link.String())
	fmt.Fprintln(w, "The code is single use. It stops working once a device enrols or it expires.")
	return code, nil
}
