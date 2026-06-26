package hw

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DMCryptMounter implements container.Mounter using dm-crypt (LUKS) via cryptsetup. Each account's
// equal-size container is a LUKS volume; the key that opens it is the TPM-unsealed account master
// key (NOT the PIN directly , the PIN unseals the key, the key opens the volume). So mounting needs
// the unsealed key, which is why the unlock flow does TPM unseal THEN mount.
//
// Layout (matches setup/debian): containers live at <stateDir>/containers/slot<N>.img, mapped to
// /dev/mapper/ghost-slot<N>, mounted at <stateDir>/mnt/slot<N>.
//
// NOT validated in CI (needs root + cryptsetup + real volumes). Built against the real cryptsetup
// CLI; exercise on the box.

type DMCryptMounter struct {
	stateDir string
	// keyFor returns the TPM-unsealed master key for a slot. Wired to the TPM SealedKey per slot.
	keyFor func(slot int, pin string) ([]byte, error)
}

func NewDMCryptMounter(stateDir string, keyFor func(slot int, pin string) ([]byte, error)) *DMCryptMounter {
	return &DMCryptMounter{stateDir: stateDir, keyFor: keyFor}
}

func (m *DMCryptMounter) imgPath(slot int) string {
	return filepath.Join(m.stateDir, "containers", fmt.Sprintf("slot%d.img", slot))
}
func (m *DMCryptMounter) mapperName(slot int) string { return fmt.Sprintf("ghost-slot%d", slot) }
func (m *DMCryptMounter) mapperPath(slot int) string { return "/dev/mapper/" + m.mapperName(slot) }
func (m *DMCryptMounter) mountPath(slot int) string {
	return filepath.Join(m.stateDir, "mnt", fmt.Sprintf("slot%d", slot))
}

// MountPath is the public accessor the datastore uses to find a slot's mounted volume.
func (m *DMCryptMounter) MountPath(slot int) string { return m.mountPath(slot) }

// Mount unseals the account key (caller passes the PIN), opens the LUKS volume with it, and mounts
// the filesystem. The key is passed to cryptsetup via stdin (a key file descriptor), never on the
// command line, and zeroised after.
func (m *DMCryptMounter) Mount(slot int, pin string) (string, error) {
	key, err := m.keyFor(slot, pin)
	if err != nil {
		return "", fmt.Errorf("unseal key for slot %d: %w", slot, err)
	}
	defer zero(key)
	return m.MapWithKey(slot, key)
}

// MapWithKey opens the LUKS volume with an already-unsealed key and mounts the filesystem. This lets
// the unlock flow unseal the key once (the Unseal stage) and reuse it here, rather than unsealing
// twice. The caller owns and zeroises key.
func (m *DMCryptMounter) MapWithKey(slot int, key []byte) (string, error) {
	mapper := m.mapperName(slot)
	// Already open? cryptsetup status returns 0 if active.
	if exec.Command("cryptsetup", "status", mapper).Run() == nil {
		return m.ensureMounted(slot)
	}
	// luksOpen reading the key from stdin (--key-file=-).
	open := exec.Command("cryptsetup", "luksOpen", "--key-file=-", m.imgPath(slot), mapper)
	open.Stdin = strings.NewReader(string(key))
	if out, err := open.CombinedOutput(); err != nil {
		return "", fmt.Errorf("luksOpen slot %d: %v: %s", slot, err, strings.TrimSpace(string(out)))
	}
	return m.ensureMounted(slot)
}

// IsMounted reports whether the slot's filesystem is currently mounted (a warm account).
func (m *DMCryptMounter) IsMounted(slot int) bool { return isMountpoint(m.mountPath(slot)) }

func (m *DMCryptMounter) ensureMounted(slot int) (string, error) {
	mnt := m.mountPath(slot)
	if err := os.MkdirAll(mnt, 0o700); err != nil {
		return "", err
	}
	// Mounted already?
	if isMountpoint(mnt) {
		return mnt, nil
	}
	mount := exec.Command("mount", m.mapperPath(slot), mnt)
	if out, err := mount.CombinedOutput(); err != nil {
		return "", fmt.Errorf("mount slot %d: %v: %s", slot, err, strings.TrimSpace(string(out)))
	}
	return mnt, nil
}

// Unmount unmounts the filesystem and closes the LUKS mapping, so the key is no longer resident.
func (m *DMCryptMounter) Unmount(slot int) error {
	mnt := m.mountPath(slot)
	if isMountpoint(mnt) {
		if out, err := exec.Command("umount", mnt).CombinedOutput(); err != nil {
			return fmt.Errorf("umount slot %d: %v: %s", slot, err, strings.TrimSpace(string(out)))
		}
	}
	mapper := m.mapperName(slot)
	if exec.Command("cryptsetup", "status", mapper).Run() == nil {
		if out, err := exec.Command("cryptsetup", "luksClose", mapper).CombinedOutput(); err != nil {
			return fmt.Errorf("luksClose slot %d: %v: %s", slot, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// ResizeToFill grows the account's filesystem to fill its container, with the account's own key
// already applied (the volume is open). Per-account and key-independent, so a decoy looks full when
// its owner opens it without ever touching another account's key.
func (m *DMCryptMounter) ResizeToFill(slot int) error {
	// resize2fs on the open mapper device extends ext4 to the device size. (The container itself was
	// grown in lockstep, keylessly, by GrowAll; this just lets the filesystem use the new space.)
	if out, err := exec.Command("resize2fs", m.mapperPath(slot)).CombinedOutput(); err != nil {
		return fmt.Errorf("resize slot %d: %v: %s", slot, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isMountpoint(path string) bool {
	return exec.Command("mountpoint", "-q", path).Run() == nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
