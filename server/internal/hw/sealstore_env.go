package hw

// EnvSealStore persists the software tier's salt and wrapped AMKs in a shell-sourceable env file
// (seal.env), separate from ghost.env on purpose: ghost.env is operator-editable config, seal.env is
// machine-written key material that no human should hand-edit. Format is KEY=base64 lines:
//
//	GHOST_SEAL_MODE=software
//	GHOST_SEAL_SALT=<base64>
//	GHOST_SEAL_SLOT0=<base64 nonce||ct||tag>
//	GHOST_SEAL_SLOT1=<base64 ...>
//
// The TPM tier writes only GHOST_SEAL_MODE=tpm here and no salt/slots (its wrapped key lives in the
// TPM). Read-modify-write is whole-file for simplicity and because writes are rare (provision, PIN
// change, migrate); concurrent writers are not a case that arises.

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type EnvSealStore struct{ path string }

func NewEnvSealStore(path string) *EnvSealStore { return &EnvSealStore{path: path} }

const (
	keyMode = "GHOST_SEAL_MODE"
	keySalt = "GHOST_SEAL_SALT"
	keySlot = "GHOST_SEAL_SLOT" // + slot number
)

func (e *EnvSealStore) read() (map[string]string, error) {
	m := map[string]string{}
	f, err := os.Open(e.path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		m[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return m, sc.Err()
}

// write rewrites the file atomically (temp + rename) at 0600. seal.env holds key material, so it is
// tighter than ghost.env's 0640 , only the owner (the service user) ever needs it.
func (e *EnvSealStore) write(m map[string]string) error {
	var b strings.Builder
	b.WriteString("# LocalGhost seal material , machine-written, do not hand-edit.\n")
	// Deterministic order: mode, salt, then slots ascending, so diffs are stable.
	if v, ok := m[keyMode]; ok {
		fmt.Fprintf(&b, "%s=%s\n", keyMode, v)
	}
	if v, ok := m[keySalt]; ok {
		fmt.Fprintf(&b, "%s=%s\n", keySalt, v)
	}
	for slot := 0; slot < 16; slot++ {
		if v, ok := m[fmt.Sprintf("%s%d", keySlot, slot)]; ok {
			fmt.Fprintf(&b, "%s%d=%s\n", keySlot, slot, v)
		}
	}
	tmp := e.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, e.path)
}

func (e *EnvSealStore) set(key, valB64 string) error {
	m, err := e.read()
	if err != nil {
		return err
	}
	m[key] = valB64
	return e.write(m)
}

func (e *EnvSealStore) Salt() ([]byte, error) {
	m, err := e.read()
	if err != nil {
		return nil, err
	}
	v, ok := m[keySalt]
	if !ok {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(v)
}

func (e *EnvSealStore) SetSalt(salt []byte) error {
	return e.set(keySalt, base64.StdEncoding.EncodeToString(salt))
}

func (e *EnvSealStore) Wrapped(slot int) ([]byte, error) {
	m, err := e.read()
	if err != nil {
		return nil, err
	}
	v, ok := m[keySlot+strconv.Itoa(slot)]
	if !ok {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(v)
}

func (e *EnvSealStore) SetWrapped(slot int, blob []byte) error {
	return e.set(keySlot+strconv.Itoa(slot), base64.StdEncoding.EncodeToString(blob))
}

func (e *EnvSealStore) DeleteWrapped(slot int) error {
	m, err := e.read()
	if err != nil {
		return err
	}
	delete(m, keySlot+strconv.Itoa(slot))
	return e.write(m)
}

// DeleteSalt removes the software tier's salt. Used by migrate-to-tpm cleanup, after the mode flip:
// with mode=tpm the salt is dead weight, and leaving it invites "why is there a salt on a tpm box"
// confusion when inspecting the file.
func (e *EnvSealStore) DeleteSalt() error {
	m, err := e.read()
	if err != nil {
		return err
	}
	delete(m, keySalt)
	return e.write(m)
}

// Mode reads GHOST_SEAL_MODE (tpm|software|""). The daemon uses it to pick the Sealer at unlock.
func (e *EnvSealStore) Mode() (string, error) {
	m, err := e.read()
	if err != nil {
		return "", err
	}
	return m[keyMode], nil
}

// SetMode records the tier. Setup and the migrate commands call this.
func (e *EnvSealStore) SetMode(mode string) error {
	m, err := e.read()
	if err != nil {
		return err
	}
	m[keyMode] = mode
	return e.write(m)
}
