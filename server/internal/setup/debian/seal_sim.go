//go:build !tpm

package debian

import (
	"fmt"
	"os"

	"golang.org/x/crypto/argon2"
)

// sealAndFormat (simulation build).
//
// This build has no TPM, so there is nothing to seal a random AMK against. Rather than refuse
// outright (which left developers unable to exercise the full enrol/unlock/serve loop without box
// hardware), the sim provisions a REAL LUKS container keyed by a PIN-derived key , Argon2id over
// pinAuth(PIN). The disk genuinely encrypts; what it lacks is the hardware wall.
//
// The security difference from the TPM build, stated without softening, because pretending otherwise
// is the one thing this project does not do:
//
//   - TPM build: the LUKS key is a random 256-bit AMK sealed in hardware, released only on a correct
//     PIN, with a hardware dictionary-attack lockout. A short PIN is safe and an offline disk attack
//     is useless , the key is not derivable from the PIN.
//   - Sim build: the LUKS key IS derived from the PIN. Argon2id is a strong KDF, but an attacker with
//     the raw disk can brute-force the PIN offline at whatever rate their hardware allows, and there
//     is no lockout. A short PIN is NOT safe here. This is a DEVELOPMENT convenience, not a secure
//     deployment.
//
// It is gated behind --insecure-sim on ghost-setup (System.InsecureSim) and refuses to run without
// it, so nobody provisions an insecure box by forgetting a build tag. The daemon side must derive the
// SAME key to open the container at unlock: simBackend.Unseal has to return SimDiskKey(pin), not
// zeros , see the note there. If they disagree, the container will not open and that is the correct
// failure (better a dev box that will not unlock than a false sense of a working secure flow).
func (s *System) sealAndFormat() error {
	if !s.InsecureSim {
		return fmt.Errorf(
			"provisioning needs the TPM-backed build (make box TAGS=tpm) on real hardware. To provision " +
				"an INSECURE, PIN-derived-key container for development, pass --insecure-sim , the disk " +
				"key will be derivable from the PIN with no hardware lockout")
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  ############################################################")
	fmt.Fprintln(os.Stderr, "  #  SIM BUILD , NOT SECURE                                  #")
	fmt.Fprintln(os.Stderr, "  #  The LUKS key is DERIVED FROM THE PIN, not TPM-sealed.   #")
	fmt.Fprintln(os.Stderr, "  #  No hardware lockout. A short PIN is brute-forceable     #")
	fmt.Fprintln(os.Stderr, "  #  offline from the raw disk. Development only.            #")
	fmt.Fprintln(os.Stderr, "  ############################################################")
	fmt.Fprintln(os.Stderr, "")

	key := SimDiskKey(s.MainPIN)
	defer zeroBytes(key)
	if err := s.formatLUKS(key); err != nil {
		return err
	}
	return nil
}

// SimDiskKey derives the sim build's 32-byte LUKS key from the PIN. Exported because the daemon's
// sim backend must reproduce it to open the container at unlock. Argon2id params are the interactive
// defaults from the RFC draft (t=1, 64MiB, p=4) , enough to make offline PIN search costly without
// making every dev unlock sluggish. The salt is a FIXED domain-separation string, not a random
// per-box salt: the daemon has no shared secret with setup to recover a random salt from, and in the
// sim build the salt is not the security boundary anyway (the PIN's entropy is). In the TPM build the
// AMK is random and this function is not used.
func SimDiskKey(pin string) []byte {
	const simSalt = "localghost/sim-disk/v1"
	return argon2.IDKey(pinAuthBytes(pin), []byte(simSalt), 1, 64*1024, 4, 32)
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
