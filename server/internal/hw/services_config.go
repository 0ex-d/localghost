package hw

// ServicesConfig is the single operational description of what a box runs, living on the encrypted
// volume (<mount>/services.conf). One file to inspect a box: the daemon health ports, and the
// Postgres + Redis ports and generated passwords. It is crypto-erased with the volume, so the
// passwords never touch the unencrypted system drive.
//
// Written once at provision (WriteServicesConfig), read by DataStore at Start (for pg/redis) and by
// the secd supervisor at unlock (for the daemon health ports). Centralised on purpose , Vlad's call:
// operationally, one file answers "what ports and credentials does this box use", instead of the
// ports being derived in code and the passwords generated at first-run in a second file.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type DBConfig struct {
	Port     int    `json:"port"`
	Password string `json:"password"` // generated at provision; gates TCP
	User     string `json:"user"`     // postgres role / redis has no user, password only
	Name     string `json:"name"`     // postgres database name
}

type ServicesConfig struct {
	Postgres DBConfig       `json:"postgres"`
	Redis    DBConfig       `json:"redis"`
	Daemons  map[string]int `json:"daemons"` // service name -> loopback health port
}

// DefaultServicesConfig builds a fresh config with generated passwords and the fixed loopback ports.
// Called at provision for the single account (slot 0). Ports are fixed (not derived) so the file is
// the whole truth; a multi-account future would offset them per slot here, in one place.
func DefaultServicesConfig() (ServicesConfig, error) {
	pgPw, err := randHex(32)
	if err != nil {
		return ServicesConfig{}, err
	}
	redisPw, err := randHex(32)
	if err != nil {
		return ServicesConfig{}, err
	}
	return ServicesConfig{
		Postgres: DBConfig{Port: 6000, Password: pgPw, User: "ghost", Name: "ghost"},
		Redis:    DBConfig{Port: 6100, Password: redisPw},
		Daemons: map[string]int{
			"ghost.shadowd": 9110,
			"ghost.watchd":  9111,
			"ghost.framed":  9112,
			"ghost.noted":   9113,
			"ghost.synthd":  9114,
			"ghost.tallyd":  9115,
			"ghost.voiced":  9116,
			"ghost.cued":    9117,
		},
	}, nil
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("entropy: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// WriteServicesConfig writes the config into the mounted volume at 0600. Called at provision, after
// the volume is mounted for the first time.
func WriteServicesConfig(mountPath string, c ServicesConfig) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(mountPath, "services.conf"), b, 0o600)
}

// LoadServicesConfig reads the config from the mounted volume. Returns an error if absent , unlike
// the earlier daemon-only default, the pg/redis passwords cannot be defaulted (they must match what
// initdb was given), so a missing file on a provisioned box is a real fault, not a fallback.
func LoadServicesConfig(mountPath string) (ServicesConfig, error) {
	b, err := os.ReadFile(filepath.Join(mountPath, "services.conf"))
	if err != nil {
		return ServicesConfig{}, fmt.Errorf("read services.conf: %w", err)
	}
	var c ServicesConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return ServicesConfig{}, fmt.Errorf("parse services.conf: %w", err)
	}
	return c, nil
}
