//go:build !tpm

package debian

import "fmt"

// sealAndFormat (simulation build): provisioning real encrypted storage needs the TPM to seal the
// AMK, which is only available in the -tags tpm build. Refuse clearly rather than pretend. Run
// `make box TAGS=tpm` and provision from that binary on the real box.
func (s *System) sealAndFormat() error {
	return fmt.Errorf("provisioning needs the TPM-backed build: rebuild with -tags tpm (make box) and run on the box")
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
