package pair

import (
	"fmt"
	"io"
)

// Options for one enrolment-QR render. ghost-setup fills these from config/flags.
type Options struct {
	Host     string // LAN address or .local; empty -> auto-detect
	Port     int    // mTLS port the box serves on
	CertPath string // PEM cert served on that port; its SHA-256 is the trust anchor
	BoxName  string // human label (defaults to hostname elsewhere)
	// IssueDevice mints a device client identity (cert + PKCS8 key, raw DER) for delivery inside
	// the QR. A seam, like encodeQR, so this package never imports the OS-specific PKI. When set,
	// the QR carries the identity and enrolment is one scan with no network issuance; when nil the
	// link is code-only (the app will refuse to enrol from it , stated in its EnrollLink contract).
	// No name: the server does not care which phone this is. Naming is app-side metadata, and key
	// rotation is just rendering a new QR , a fresh identity every time, no registry to keep.
	IssueDevice func() (certDER, keyDER []byte, err error)
}

// Run builds the enroll link and writes both the link text and a scannable terminal QR to w. There
// is no pairing code: the QR carries the device identity, scanning is enrolment, and nothing on the
// daemon needs arming.
//
// EncodeQR is the seam: it turns the link string into a Matrix. See qrencode.go, the
// from-scratch byte-mode encoder in qrencode.go (no third-party QR code).
//
// Key handling, honestly: when IssueDevice is set, the device private key exists here in memory
// and inside the rendered QR on the terminal , that QR IS the credential and the screen is showing
// it, which is the point of screen-to-camera delivery. What this function guarantees is narrower
// and checkable: the key is never passed anywhere that persists it, and the local buffer is zeroed
// before returning. Zeroing in Go is best-effort (the base64 copy inside the link string is not
// reachable for scrubbing); the durable guarantee is "never on disk", enforced in the issuer.
func Run(w io.Writer, opts Options, encodeQR func(string) (Matrix, error)) (err error) {
	host := opts.Host
	if host == "" {
		if host, err = LANHost(); err != nil {
			return err
		}
	}
	fp, err := CertFingerprint(opts.CertPath)
	if err != nil {
		return fmt.Errorf("reading cert fingerprint: %w", err)
	}

	link := EnrollLink{
		Host:        host,
		Port:        opts.Port,
		Fingerprint: fp,
		BoxName:     opts.BoxName,
	}
	if opts.IssueDevice != nil {
		certDER, keyDER, err := opts.IssueDevice()
		if err != nil {
			return fmt.Errorf("issuing device identity: %w", err)
		}
		link.DeviceCertDER = certDER
		link.DeviceKeyDER = keyDER
		defer func() {
			for i := range keyDER {
				keyDER[i] = 0
			}
		}()
	}
	matrix, err := encodeQR(link.String())
	if err != nil {
		return fmt.Errorf("encoding QR: %w", err)
	}

	fmt.Fprintln(w)
	// A cert-bearing enroll link is ~400 bytes -> a v15 symbol, 85 half-block columns with the quiet
	// zone: wider than a standard 80-column terminal. Fit to the real width, dropping to 2x2 quadrant
	// blocks when half-blocks would clip (a clipped QR does not scan). When even that does not fit,
	// print the symbol anyway and say exactly how wide the terminal needs to be, rather than failing
	// silently or clipping without comment.
	cols := TerminalCols()
	rendered, needCols := RenderTerminalFit(matrix, cols)
	fmt.Fprintln(w, rendered)
	if cols > 0 && needCols > cols {
		fmt.Fprintf(w, "This terminal is %d columns wide but the QR needs %d; widen the window or the code will not scan.\n", cols, needCols)
		fmt.Fprintln(w, "If the QR looks like broken boxes, the console font lacks quadrant glyphs; use a terminal emulator over SSH.")
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "Scan this with the LocalGhost app. Scanning IS enrolment: the QR carries the")
	fmt.Fprintln(w, "device certificate and key, so treat it as a credential and clear the screen after.")
	fmt.Fprintf(w, "  box     %s:%d\n", host, opts.Port)
	fmt.Fprintf(w, "  finger  %s\n", fp)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Link:", link.String())
	return nil
}
