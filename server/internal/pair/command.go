package pair

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"
)

// Options for one pairing render. The daemon/setup fills these from config/flags.
type Options struct {
	Host     string // LAN address or .local; empty -> auto-detect
	Port     int    // mTLS port the box serves on
	CertPath string // PEM cert served on that port; its SHA-256 is the trust anchor
	BoxName  string // human label (defaults to hostname elsewhere)
	// IssueDevice mints the device's client cert + key as raw DER. Required , the QR carries the
	// identity, so there is nothing to render without it. Wired to PKI.IssueDeviceCertDER.
	IssueDevice func(name string) (certDER, keyDER []byte, err error)
	// Animate rotates multi-frame QRs on the terminal instead of printing them in a column. The app
	// assembles frames in any order, so the person just holds the phone up while the box cycles ,
	// no taps, no network, no feedback channel needed (and none is possible pre-enrolment: the phone
	// has no client cert until the scan completes, so nginx's mTLS wall rejects it by design).
	// Callers set this when stdout is an interactive tty.
	Animate bool
}

// Run mints a fresh device identity, builds the enroll link that CARRIES it, and writes the link
// text plus a scannable terminal QR to w. There is no pairing code and no return value but error:
// scanning the QR is enrolment, done locally on the phone, so the box has nothing to "arm" or track.
//
// EncodeQR is the seam: it turns a frame string into a Matrix (qrencode.go, the from-scratch
// byte-mode encoder, no third-party QR). The device identity (cert+key) is ~1.4 KB, too much for one
// comfortably-scannable QR, so ChunkLink splits it into a few small frames the app reassembles.
func Run(w io.Writer, opts Options, encodeQR func(string) (Matrix, error)) error {
	host := opts.Host
	if host == "" {
		var err error
		if host, err = LANHost(); err != nil {
			return err
		}
	}
	fp, err := CertFingerprint(opts.CertPath)
	if err != nil {
		return fmt.Errorf("reading cert fingerprint: %w", err)
	}
	if opts.IssueDevice == nil {
		return fmt.Errorf("no device issuer wired: cannot mint the identity the QR must carry")
	}
	certDER, keyDER, err := opts.IssueDevice("primary")
	if err != nil {
		return fmt.Errorf("issuing device cert: %w", err)
	}

	link := EnrollLink{
		Host:          host,
		Port:          opts.Port,
		Fingerprint:   fp,
		BoxName:       opts.BoxName,
		DeviceCertDER: certDER,
		DeviceKeyDER:  keyDER,
	}
	// Chunk the link into scannable frames. A real device identity (~1.4 KB) will not fit one
	// comfortable QR, so we render a few small frames the app scans in sequence. A small link yields a
	// single frame, so this is one code path, not a special case.
	frames := ChunkLink(link.String())
	fmt.Fprintln(w)
	if len(frames) == 1 {
		matrix, err := encodeQR(frames[0])
		if err != nil {
			return fmt.Errorf("encoding QR: %w", err)
		}
		fmt.Fprintln(w, RenderTerminal(matrix))
		fmt.Fprintln(w, "Scan this with the LocalGhost app. The QR carries the device identity , scanning it enrols the phone.")
	} else if opts.Animate {
		if err := animateFrames(w, frames, encodeQR); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(w, "The device identity spans %d QR codes. In the app, scan them in any order , it\n", len(frames))
		fmt.Fprintln(w, "shows progress and assembles the identity once all are captured.")
		for i, frame := range frames {
			matrix, err := encodeQR(frame)
			if err != nil {
				return fmt.Errorf("encoding QR frame %d: %w", i+1, err)
			}
			fmt.Fprintf(w, "\n--- QR %d of %d ---\n", i+1, len(frames))
			fmt.Fprintln(w, RenderTerminal(matrix))
		}
	}
	fmt.Fprintf(w, "  box     %s:%d\n", host, opts.Port)
	fmt.Fprintf(w, "  finger  %s\n", fp)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Link:", link.String())
	fmt.Fprintln(w, "Anyone who scans this QR gets a working device identity , show it to your phone only.")
	return nil
}

// animateFrames rotates the enrolment QR frames on an interactive terminal: each frame shows for a
// couple of seconds, then the screen clears and the next appears, looping until the operator presses
// Enter. The app collects frames opportunistically in any order (FrameAssembler is order-independent
// and duplicate-safe), so the person just holds the phone steady; its "scanned N of M" counter says
// when the set is complete. No feedback channel exists , or can: pre-enrolment the phone has no
// client cert, so the box's mTLS edge rejects it, which is the appears-down design doing its job.
// The rotation is pure display; security posture is unchanged.
func animateFrames(w io.Writer, frames []string, encodeQR func(string) (Matrix, error)) error {
	// Pre-encode every frame so the loop never fails mid-rotation.
	rendered := make([]string, len(frames))
	for i, f := range frames {
		m, err := encodeQR(f)
		if err != nil {
			return fmt.Errorf("encoding QR frame %d: %w", i+1, err)
		}
		rendered[i] = RenderTerminal(m)
	}
	done := make(chan struct{})
	go func() {
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		close(done)
	}()
	const hold = 2200 * time.Millisecond
	i := 0
	for {
		// ANSI clear + home; the next frame redraws in place.
		fmt.Fprint(w, "\033[2J\033[H")
		fmt.Fprintf(w, "QR %d of %d , hold the phone steady; the app collects them in any order.\n", i%len(frames)+1, len(frames))
		fmt.Fprintln(w, "Press Enter here once the app shows all frames captured.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, rendered[i%len(frames)])
		select {
		case <-done:
			fmt.Fprint(w, "\033[2J\033[H")
			return nil
		case <-time.After(hold):
			i++
		}
	}
}
