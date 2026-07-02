package hw

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// MuteStore reads and writes the notification mute for a mounted account. The mute is PER SCOPE:
//   - scope "*"            the GLOBAL mute , overrides everything (mutes all services)
//   - scope "ghost.synthd" a PER-SERVICE mute , mutes just that notification-producing daemon
//
// A service is muted if the global mute is active OR its own per-service mute is active. The mute
// lives in the in-volume Postgres (notification_mute(scope, muted_until), authoritative + durable)
// and is cached in the in-volume Redis (the poller reads Redis on every ~15-min fetch). Both run
// inside the encrypted volume, so the mute is only readable while unlocked , correct, since while
// locked the app is down anyway.
//
// Durations are resolved to an absolute `until` timestamp by the caller (presets 1h/1d/1w/forever or a
// custom length), so the store only deals in timestamps. "forever" is a far-future timestamp.
//
// Shell-out to redis-cli / psql, matching DataStore, so no DB Go-driver dependency is added.
//
// Redis keys:  notifications:mute:*            -> unix seconds | "forever" | absent (not muted)
//              notifications:mute:ghost.synthd -> same, per service
// Postgres:    notification_mute(scope) rows
//
// NOT validated in CI; exercise on the box.

const (
	muteGlobalScope = "*"
	foreverSentinel = "forever"
)

// foreverTime is the far-future timestamp used for "mute forever".
var foreverTime = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

type MuteStore struct {
	pgSocketFor func(slot int) string
}

func NewMuteStore(pgSocketFor func(slot int) string) *MuteStore {
	return &MuteStore{pgSocketFor: pgSocketFor}
}

func redisMuteKey(scope string) string { return "notifications:mute:" + scope }

// IsMuted reports whether the given service is currently muted: true if the GLOBAL mute is active OR
// the service's own per-service mute is active. Reads the Redis cache (the poller's fast path). On any
// error it returns false ("not muted") so a failure surfaces notifications rather than silently
// suppressing them.
func (m *MuteStore) IsMuted(slot int, service string) bool {
	if m.scopeActive(slot, muteGlobalScope) {
		return true
	}
	if service == "" || service == muteGlobalScope {
		return false
	}
	return m.scopeActive(slot, service)
}

// GlobalMuted reports whether the global mute alone is active.
func (m *MuteStore) GlobalMuted(slot int) bool { return m.scopeActive(slot, muteGlobalScope) }

func (m *MuteStore) scopeActive(slot int, scope string) bool {
	out, err := exec.Command("redis-cli", "-p", strconv.Itoa(redisPort(slot)), "GET", redisMuteKey(scope)).Output()
	if err != nil {
		return false
	}
	v := strings.TrimSpace(string(out))
	if v == "" || v == "(nil)" {
		return false
	}
	if v == foreverSentinel {
		return true
	}
	ts, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Before(time.Unix(ts, 0))
}

// SetMute mutes the given scope ("*" for global, or a service name) until `until`. A zero `until`
// clears it. Postgres (durable) is written first, then Redis (cache).
func (m *MuteStore) SetMute(slot int, scope string, until time.Time) error {
	if until.IsZero() {
		return m.ClearMute(slot, scope)
	}
	if err := m.writePostgres(slot, scope, &until); err != nil {
		return err
	}
	val := strconv.FormatInt(until.Unix(), 10)
	if !until.Before(foreverTime.Add(-time.Hour)) {
		val = foreverSentinel
	}
	return m.writeRedis(slot, "SET", redisMuteKey(scope), val)
}

// MuteForever mutes the given scope indefinitely.
func (m *MuteStore) MuteForever(slot int, scope string) error {
	if err := m.writePostgres(slot, scope, &foreverTime); err != nil {
		return err
	}
	return m.writeRedis(slot, "SET", redisMuteKey(scope), foreverSentinel)
}

// ClearMute re-enables the given scope.
func (m *MuteStore) ClearMute(slot int, scope string) error {
	if err := m.deletePostgres(slot, scope); err != nil {
		return err
	}
	return m.writeRedis(slot, "DEL", redisMuteKey(scope))
}

// Status returns the active mutes (scope -> until) for the settings screen, from Postgres (the
// authoritative view), filtering out expired rows.
func (m *MuteStore) Status(slot int) (map[string]time.Time, error) {
	sock := m.pgSocketFor(slot)
	port := strconv.Itoa(pgPort(slot))
	q := "SELECT scope, extract(epoch from muted_until)::bigint FROM notification_mute WHERE muted_until > now();"
	out, err := exec.Command("psql", "-h", sock, "-p", port, "-U", "ghost", "-d", "ghost",
		"-At", "-F", "|", "-c", q).Output()
	if err != nil {
		return nil, fmt.Errorf("mute status: %w", err)
	}
	res := map[string]time.Time{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		ts, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		res[parts[0]] = time.Unix(ts, 0)
	}
	return res, nil
}

func (m *MuteStore) writePostgres(slot int, scope string, until *time.Time) error {
	sock := m.pgSocketFor(slot)
	port := strconv.Itoa(pgPort(slot))
	stmt := fmt.Sprintf(
		"INSERT INTO notification_mute (scope, muted_until) VALUES ('%s', to_timestamp(%d)) "+
			"ON CONFLICT (scope) DO UPDATE SET muted_until = EXCLUDED.muted_until;",
		sqlEscape(scope), until.Unix())
	out, err := exec.Command("psql", "-h", sock, "-p", port, "-U", "ghost", "-d", "ghost",
		"-v", "ON_ERROR_STOP=1", "-c", stmt).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mute postgres write: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *MuteStore) deletePostgres(slot int, scope string) error {
	sock := m.pgSocketFor(slot)
	port := strconv.Itoa(pgPort(slot))
	stmt := fmt.Sprintf("DELETE FROM notification_mute WHERE scope = '%s';", sqlEscape(scope))
	out, err := exec.Command("psql", "-h", sock, "-p", port, "-U", "ghost", "-d", "ghost",
		"-v", "ON_ERROR_STOP=1", "-c", stmt).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mute postgres delete: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *MuteStore) writeRedis(slot int, args ...string) error {
	full := append([]string{"-p", strconv.Itoa(redisPort(slot))}, args...)
	out, err := exec.Command("redis-cli", full...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mute redis write: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sqlEscape doubles single quotes. Scopes are validated against the known daemon set by the handler
// before reaching here; this is belt-and-braces against a malformed scope.
func sqlEscape(s string) string { return strings.ReplaceAll(s, "'", "''") }

// SocketForMount builds the postgres socket dir from a mount path (matching DataStore.pgData).
func SocketForMount(mountPath string) string { return filepath.Join(mountPath, "postgres") }
