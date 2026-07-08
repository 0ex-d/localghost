// Package svcconf is the per-service config file (<mount>/conf/<service>.conf) and the base control
// commands every daemon exposes on its ctlsock. Modelled on how redis reads redis.conf and reloads
// parts of it live: some keys apply immediately on reload (log level, disk-guard thresholds), and the
// reload response reports which keys were applied vs which need a restart, rather than pretending all
// took effect.
//
// A daemon embeds a Base in its own conf struct for the shared keys, wires the base commands with
// BindBase, and adds its service-specific keys and commands on top.
package svcconf

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/LocalGhostDao/localghost/server/internal/ctlsock"
)

// Base is the config every service shares. Embed it in a service's conf struct.
type Base struct {
	// LogLevel is hot-reloadable: debug|info|warn|error. Applied live via the service's LevelVar.
	LogLevel string `json:"logLevel"`

	// Log disk-guard (watchd uses these; harmless on others). SoftCapMB is when watchd starts asking
	// ghost.oracled whether it is over-logging; HardCapMB is the dumb backstop where it prunes
	// oldest-first regardless, because a full volume wedges the box.
	LogSoftCapMB int `json:"logSoftCapMB"`
	LogHardCapMB int `json:"logHardCapMB"`

	// RetentionDays is how many days of gzipped archives to keep. Hot-reloadable.
	RetentionDays int `json:"retentionDays"`
}

// DefaultBase is the fallback when a conf file is absent or a field is zero.
func DefaultBase() Base {
	return Base{LogLevel: "info", LogSoftCapMB: 512, LogHardCapMB: 1024, RetentionDays: 7}
}

// Path returns <mount>/conf/<service>.conf.
func Path(mount, service string) string {
	return filepath.Join(mount, "conf", service+".conf")
}

// Load reads and unmarshals a service conf into v (which should embed Base). A missing file is NOT an
// error , the caller seeds defaults first, so a fresh box with no conf runs on defaults. Zero-value
// base fields are filled from DefaultBase by the caller via FillBaseDefaults.
func Load(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(b, v)
}

// Save writes v as pretty JSON to path (used by provisioning to seed a default conf).
func Save(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o640)
}

// FillBaseDefaults replaces zero base fields with DefaultBase values, so a partial conf is valid.
func FillBaseDefaults(b *Base) {
	d := DefaultBase()
	if b.LogLevel == "" {
		b.LogLevel = d.LogLevel
	}
	if b.LogSoftCapMB == 0 {
		b.LogSoftCapMB = d.LogSoftCapMB
	}
	if b.LogHardCapMB == 0 {
		b.LogHardCapMB = d.LogHardCapMB
	}
	if b.RetentionDays == 0 {
		b.RetentionDays = d.RetentionDays
	}
}

// ApplyLevel maps a level string to slog and sets it on lv live. Unknown strings leave it unchanged
// and return an error so a bad reload is reported, not silently ignored.
func ApplyLevel(lv *slog.LevelVar, level string) error {
	switch level {
	case "debug":
		lv.Set(slog.LevelDebug)
	case "info":
		lv.Set(slog.LevelInfo)
	case "warn":
		lv.Set(slog.LevelWarn)
	case "error":
		lv.Set(slog.LevelError)
	default:
		return fmt.Errorf("unknown log level %q", level)
	}
	return nil
}

// Reloader is what BindBase needs from a daemon: re-read its conf from disk and return the base view
// plus a per-key applied/needs-restart report. The daemon owns decoding its full conf; it returns the
// Base and a map of key->status for the reload response.
type Reloader func() (Base, map[string]string, error)

// BindBase wires the base commands onto a daemon's ctlsock server:
//
//	ping                -> "pong"
//	commands            -> the list of registered command names
//	log-level [<level>] -> get, or set live via the LevelVar
//	reload              -> re-read the conf; apply hot keys; report applied vs needs-restart
//	status              -> the base status (level, caps); a daemon can override with richer status
//
// lv is the daemon's live log level. reload is the daemon's conf re-reader.
func BindBase(srv *ctlsock.Server, service string, lv *slog.LevelVar, reload Reloader) {
	srv.Handle("ping", func(json.RawMessage) (ctlsock.Response, error) {
		return ctlsock.Response{OK: true, Text: "pong"}, nil
	})

	srv.Handle("commands", func(json.RawMessage) (ctlsock.Response, error) {
		data, _ := json.Marshal(srv.Commands())
		return ctlsock.Response{OK: true, Data: data}, nil
	})

	srv.Handle("log-level", func(args json.RawMessage) (ctlsock.Response, error) {
		var a struct {
			Level string `json:"level"`
		}
		if len(args) > 0 {
			_ = json.Unmarshal(args, &a)
		}
		if a.Level == "" {
			return ctlsock.Response{OK: true, Text: lv.Level().String()}, nil
		}
		if err := ApplyLevel(lv, a.Level); err != nil {
			return ctlsock.Response{}, err
		}
		return ctlsock.Response{OK: true, Text: "log level set to " + a.Level}, nil
	})

	srv.Handle("reload", func(json.RawMessage) (ctlsock.Response, error) {
		base, report, err := reload()
		if err != nil {
			return ctlsock.Response{}, err
		}
		// Log level is always a hot key: apply it here so every daemon reloads it uniformly.
		if err := ApplyLevel(lv, base.LogLevel); err == nil {
			if report == nil {
				report = map[string]string{}
			}
			report["logLevel"] = "applied"
		}
		data, _ := json.Marshal(report)
		return ctlsock.Response{OK: true, Text: "reloaded", Data: data}, nil
	})

	srv.Handle("status", func(json.RawMessage) (ctlsock.Response, error) {
		st := map[string]any{"service": service, "logLevel": lv.Level().String()}
		data, _ := json.Marshal(st)
		return ctlsock.Response{OK: true, Data: data}, nil
	})
}
