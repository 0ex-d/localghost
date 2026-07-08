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
	Password string `json:"password"` // legacy single password; kept for the readiness checks
	User     string `json:"user"`     // postgres superuser role (ghost); redis default user
	Name     string `json:"name"`     // postgres database name

	// Role-split service credentials. read_only can only SELECT (pg) / +@read (redis); read_write can
	// write its own tables. Daemons connect as one of these, never as the owner role. Generated at
	// provision alongside the owner password.
	ROUser string `json:"roUser"`
	ROPass string `json:"roPass"`
	RWUser string `json:"rwUser"`
	RWPass string `json:"rwPass"`
}

type ServicesConfig struct {
	Postgres DBConfig       `json:"postgres"`
	Redis    DBConfig       `json:"redis"`
	Daemons  map[string]int `json:"daemons"`  // COHORT watchd supervises: name -> loopback health port
	Critical []string       `json:"critical"` // subset of Daemons whose failure is surfaced as critical
}

// DefaultServicesConfig builds a fresh config with generated passwords and the fixed loopback ports.
// Called at provision for the single account (slot 0). Ports are fixed (not derived) so the file is
// the whole truth; a multi-account future would offset them per slot here, in one place.
func DefaultServicesConfig() (ServicesConfig, error) {
	// Six secrets: owner + read-only + read-write, per engine. All randHex, so SASLprep is a no-op and
	// none ever touches an argv (the native clients pass them in the auth handshake, not on a command
	// line , unlike the old psql/redis-cli shell-outs).
	gen := func() (string, error) { return randHex(32) }
	pgOwner, err := gen()
	if err != nil {
		return ServicesConfig{}, err
	}
	pgRO, err := gen()
	if err != nil {
		return ServicesConfig{}, err
	}
	pgRW, err := gen()
	if err != nil {
		return ServicesConfig{}, err
	}
	rdOwner, err := gen()
	if err != nil {
		return ServicesConfig{}, err
	}
	rdRO, err := gen()
	if err != nil {
		return ServicesConfig{}, err
	}
	rdRW, err := gen()
	if err != nil {
		return ServicesConfig{}, err
	}
	return ServicesConfig{
		Postgres: DBConfig{
			Port: 6000, Password: pgOwner, User: "ghost", Name: "ghost",
			ROUser: "ghost_ro", ROPass: pgRO, RWUser: "ghost_rw", RWPass: pgRW,
		},
		Redis: DBConfig{
			Port: 6100, Password: rdOwner,
			ROUser: "ghost_ro", ROPass: rdRO, RWUser: "ghost_rw", RWPass: rdRW,
		},
		// The cohort watchd supervises and the critical set both derive from the single daemon registry
		// (daemon_registry.go) , the one place a daemon is declared. ghost.watchd is NOT here (it is the
		// supervisor, started by secd directly). Postgres/Redis are not here either (secd owns them via
		// DataStore, before watchd).
		Critical: CriticalDaemons(),
		Daemons:  SupervisedDaemons(),
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
