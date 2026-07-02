package container

// A container is the account's encrypted store: a dm-crypt / LUKS volume over a backing device,
// keyed by the TPM-sealed account key. The single-account model has one container spanning the
// provisioned device , there is no on-disk deniability, no equal-size decoy invariant, and no
// lockstep growth. Deniability lives on the phone (a thin client holding only recent data), not in
// the on-disk layout. See the threat-model docs.
//
// This package defines the mount seam. The actual dm-crypt + TPM wiring is in internal/hw.

// Mounter mounts and unmounts the account's container. The implementation:
//   - unseals the account key from the TPM (PIN-gated; the TPM enforces the lockout),
//   - maps the ciphertext as a block device (dm-crypt) WITHOUT bulk-decrypting it, so mount time is
//     the TPM unseal plus key setup.
//
// This is the system seam; the dm-crypt + TPM wiring needs the box to test.
type Mounter interface {
	Mount(slot int, pin string) (mountPath string, err error)
	Unmount(slot int) error

	// ResizeToFill extends the account's filesystem to fill its container (e.g. after the backing
	// device was enlarged). It runs at mount, with the account's own key.
	ResizeToFill(slot int) error
}
