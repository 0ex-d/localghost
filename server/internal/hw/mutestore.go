package hw

import (
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/poltergres"
	"github.com/LocalGhostDao/localghost/server/internal/apparedis"
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
	mu          sync.Mutex
	rw          map[int]*poltergres.ReadWrite
	rd          map[int]*apparedis.ReadWrite
}

func NewMuteStore(pgSocketFor func(slot int) string) *MuteStore {
	return &MuteStore{
		pgSocketFor: pgSocketFor,
		rw:          map[int]*poltergres.ReadWrite{},
		rd:          map[int]*apparedis.ReadWrite{},
	}
}

func (m *MuteStore) pg(slot int) (*poltergres.ReadWrite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.rw[slot]; ok {
		return c, nil
	}
	mount := filepath.Dir(m.pgSocketFor(slot))
	cfg, err := LoadServicesConfig(mount)
	if err != nil {
		return nil, err
	}
	c := poltergres.NewReadWrite(m.pgSocketFor(slot), cfg.Postgres.Port, cfg.Postgres.RWUser, cfg.Postgres.RWPass, cfg.Postgres.Name)
	m.rw[slot] = c
	return c, nil
}

func (m *MuteStore) rds(slot int) (*apparedis.ReadWrite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.rd[slot]; ok {
		return c, nil
	}
	mount := filepath.Dir(m.pgSocketFor(slot))
	cfg, err := LoadServicesConfig(mount)
	if err != nil {
		return nil, err
	}
	c := apparedis.NewReadWrite(cfg.Redis.Port, cfg.Redis.RWUser, cfg.Redis.RWPass)
	m.rd[slot] = c
	return c, nil
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
	c, err := m.rds(slot)
	if err != nil {
		return false
	}
	v, ok, err := c.Get(redisMuteKey(scope))
	if err != nil || !ok || v == "" {
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
	c, err := m.pg(slot)
	if err != nil {
		return nil, err
	}
	rows, err := c.Query("SELECT scope, extract(epoch from muted_until)::bigint FROM notification_mute WHERE muted_until > now()")
	if err != nil {
		return nil, fmt.Errorf("mute status: %w", err)
	}
	res := map[string]time.Time{}
	for _, r := range rows.Vals {
		if len(r) != 2 || r[0] == nil || r[1] == nil {
			continue
		}
		ts, perr := strconv.ParseInt(*r[1], 10, 64)
		if perr != nil {
			continue
		}
		res[*r[0]] = time.Unix(ts, 0)
	}
	return res, nil
}

func (m *MuteStore) writePostgres(slot int, scope string, until *time.Time) error {
	c, err := m.pg(slot)
	if err != nil {
		return err
	}
	return c.Exec(
		"INSERT INTO notification_mute (scope, muted_until) VALUES ($1, to_timestamp($2)) "+
			"ON CONFLICT (scope) DO UPDATE SET muted_until = EXCLUDED.muted_until",
		scope, until.Unix())
}

func (m *MuteStore) deletePostgres(slot int, scope string) error {
	c, err := m.pg(slot)
	if err != nil {
		return err
	}
	return c.Exec("DELETE FROM notification_mute WHERE scope = $1", scope)
}

func (m *MuteStore) writeRedis(slot int, args ...string) error {
	c, err := m.rds(slot)
	if err != nil {
		return err
	}
	_, err = c.Do(args...)
	return err
}



// SocketForMount builds the postgres socket dir from a mount path (matching DataStore.pgData).
func SocketForMount(mountPath string) string { return filepath.Join(mountPath, "postgres") }
