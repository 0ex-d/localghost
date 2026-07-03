package hw

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DataStore manages the per-account Postgres + Redis instances. Each account's databases live INSIDE
// that account's encrypted container (so the data is encrypted at rest with the account key and
// vanishes on crypto-erase), and run only while the account is mounted. ghost.secd starts them on
// unlock and stops them on lock, and routes queries to the mounted account's endpoints.
//
// This is the seam the unlock flow's StartDB / StartCache stages drive. Distinct ports per slot so
// instances never collide; bound to loopback only (the account daemons reach them, nothing external).
//
// NOT validated in CI (needs postgres + redis binaries + mounted volumes). Built against pg_ctl /
// initdb / redis-server; exercise on the box.

type Endpoints struct {
	PostgresPort int
	RedisPort    int
	Socket       string // postgres unix socket dir (loopback alternative)
}

type DataStore struct {
	// mountPathFor returns where a slot's container is mounted (from the Mounter).
	mountPathFor func(slot int) string
}

func NewDataStore(mountPathFor func(slot int) string) *DataStore {
	return &DataStore{mountPathFor: mountPathFor}
}

// cfg reads services.conf from the slot's mounted volume. Ports and passwords are the file's, not
// derived , the file is the single source of truth (see services_config.go). An unreadable config on
// a mounted volume is a real error surfaced to the caller, not a silent default.
func (d *DataStore) cfg(slot int) (ServicesConfig, error) {
	return LoadServicesConfig(d.mountPathFor(slot))
}

func (d *DataStore) pgPortCfg(slot int) (int, error) {
	c, err := d.cfg(slot)
	if err != nil {
		return 0, err
	}
	return c.Postgres.Port, nil
}
func (d *DataStore) redisPortCfg(slot int) (int, error) {
	c, err := d.cfg(slot)
	if err != nil {
		return 0, err
	}
	return c.Redis.Port, nil
}

// pgPort / redisPort are the DEFAULT loopback ports, used by the notification + mute stores which
// connect to the already-running databases. They match ServicesConfig's defaults (6000/6100). NOTE
// (honest limit): if a box overrides these in services.conf, these two stores would still use the
// defaults , they do not read the config, because they run in hot paths without the mount handle.
// Today provision always writes the defaults, so they agree. If per-box port override becomes real,
// these stores must be threaded with the config port like DataStore was. Tracked, not hidden.
func pgPort(slot int) int    { return 6000 + slot }
func redisPort(slot int) int { return 6100 + slot }

func (d *DataStore) pgData(slot int) string {
	return filepath.Join(d.mountPathFor(slot), "postgres")
}
func (d *DataStore) redisDir(slot int) string {
	return filepath.Join(d.mountPathFor(slot), "redis")
}

// Start brings up the account's Postgres and Redis (initialising the cluster on first run), and
// returns the endpoints for ghost.secd to route to. Called during the unlock StartDB/StartCache
// stages, AFTER the container is mounted (so the data dirs are inside the decrypted volume).
func (d *DataStore) Start(slot int) (Endpoints, error) {
	c, err := d.cfg(slot)
	if err != nil {
		return Endpoints{}, err
	}
	if err := d.startPostgres(slot, c); err != nil {
		return Endpoints{}, err
	}
	if err := d.startRedis(slot, c); err != nil {
		_ = d.stopPostgres(slot, c)
		return Endpoints{}, err
	}
	return Endpoints{
		PostgresPort: c.Postgres.Port,
		RedisPort:    c.Redis.Port,
		Socket:       d.pgData(slot),
	}, nil
}

// Stop tears both down on lock/unmount, so nothing holds the volume open when we close it.
func (d *DataStore) Stop(slot int) error {
	c, err := d.cfg(slot)
	if err != nil {
		return err
	}
	rerr := d.stopRedis(slot, c)
	perr := d.stopPostgres(slot, c)
	if perr != nil {
		return perr
	}
	return rerr
}

// StopCache stops this slot's Redis. Split out so the lock teardown can report it as its own step.
func (d *DataStore) StopCache(slot int) error {
	c, err := d.cfg(slot)
	if err != nil {
		return err
	}
	return d.stopRedis(slot, c)
}

// StopDB stops this slot's Postgres. Split out so the lock teardown can report it as its own step.
func (d *DataStore) StopDB(slot int) error {
	c, err := d.cfg(slot)
	if err != nil {
		return err
	}
	return d.stopPostgres(slot, c)
}

func (d *DataStore) startPostgres(slot int, c ServicesConfig) error {
	data := d.pgData(slot)
	firstRun := false
	// initdb on first run (the data dir lives in the encrypted volume).
	if _, err := os.Stat(filepath.Join(data, "PG_VERSION")); os.IsNotExist(err) {
		firstRun = true
		if err := os.MkdirAll(data, 0o700); err != nil {
			return err
		}
		if out, err := exec.Command("initdb", "-D", data, "--auth=trust", "--encoding=UTF8").CombinedOutput(); err != nil {
			return fmt.Errorf("initdb slot %d: %v: %s", slot, err, strings.TrimSpace(string(out)))
		}
	}
	// start, bound to loopback on the config's port, socket inside the volume.
	opts := fmt.Sprintf("-p %d -k %s -c listen_addresses=127.0.0.1", c.Postgres.Port, data)
	cmd := exec.Command("pg_ctl", "-D", data, "-o", opts, "-w", "-t", "30", "start")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pg_ctl start slot %d: %v: %s", slot, err, strings.TrimSpace(string(out)))
	}
	// On first run only: apply the provisioned password to the ghost role and lay down the app config
	// schema. Runs AFTER start (needs a live server) and is idempotent-guarded by firstRun.
	if firstRun {
		if err := d.initPostgresAuthAndSchema(slot, c); err != nil {
			_ = d.stopPostgres(slot, c)
			return fmt.Errorf("init db auth/schema slot %d: %w", slot, err)
		}
	}
	return nil
}

// initPostgresAuthAndSchema applies the PROVISIONED password (from services.conf) to the ghost role
// and creates the app config schema. Called once at first start, while the volume is mounted. The
// password is generated at provision, not here, so services.conf remains the single source of truth.
func (d *DataStore) initPostgresAuthAndSchema(slot int, c ServicesConfig) error {
	data := d.pgData(slot)
	port := fmt.Sprint(c.Postgres.Port)

	// The password is PROVISIONED in services.conf (generated at setup), not made up here , that file
	// is the single credential store, and it must match what gates TCP. Apply it to the ghost role
	// over the trust-auth loopback socket.
	stmts := []string{
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s';", c.Postgres.User, c.Postgres.Password),
		fmt.Sprintf("CREATE DATABASE %s OWNER %s;", c.Postgres.Name, c.Postgres.User),
	}
	for _, s := range stmts {
		if out, err := exec.Command("psql", "-h", data, "-p", port, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", s).CombinedOutput(); err != nil {
			return fmt.Errorf("psql %q: %v: %s", s, err, strings.TrimSpace(string(out)))
		}
	}

	// app config schema, in the ghost database. Tables: settings (k/v), and the notification mute.
	schema := `
CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
-- notification mute, per scope. scope '*' is the global mute (overrides everything); scope
-- 'ghost.synthd' etc. is a per-service mute. muted_until: a timestamp the mute is active until;
-- a far-future value means "forever". A row's absence (or muted_until in the past) = not muted.
CREATE TABLE IF NOT EXISTS notification_mute (
  scope       TEXT PRIMARY KEY,
  muted_until TIMESTAMPTZ NOT NULL
);
-- notifications: always produced by the daemons (mute only affects push, not storage). Durable
-- history with a seen flag; deletable forever. The Redis last-100 list is the hot push cache.
CREATE TABLE IF NOT EXISTS notifications (
  id       BIGSERIAL PRIMARY KEY,
  service  TEXT NOT NULL,
  kind     TEXT NOT NULL DEFAULT 'message',
  title    TEXT NOT NULL DEFAULT '',
  body     TEXT NOT NULL DEFAULT '',
  seen     BOOLEAN NOT NULL DEFAULT FALSE,
  -- An "ask" is a notification the user can answer (ghost.cued nominations, confirmations). options
  -- is a JSON array of choices; empty means this is a passive notification (telling, not asking).
  -- answer is the chosen option once picked; answered is when. A pending ask has answer='' answered NULL.
  options  TEXT NOT NULL DEFAULT '',
  answer   TEXT NOT NULL DEFAULT '',
  answered TIMESTAMPTZ,
  created  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS notifications_id_desc ON notifications (id DESC);
`
	if out, err := exec.Command("psql", "-h", data, "-p", port, "-d", c.Postgres.Name, "-v", "ON_ERROR_STOP=1", "-c", schema).CombinedOutput(); err != nil {
		return fmt.Errorf("apply schema: %v: %s", err, strings.TrimSpace(string(out)))
	}

	// No separate credential file , services.conf (written at provision) is the single credential
	// store, read by DataStore and anything else that needs to connect. The password was applied to
	// the role above; ghost.secd reads it from services.conf to connect over TCP.
	return nil
}

func (d *DataStore) stopPostgres(slot int, _ ServicesConfig) error {
	data := d.pgData(slot)
	if _, err := os.Stat(filepath.Join(data, "postmaster.pid")); os.IsNotExist(err) {
		return nil // not running
	}
	out, err := exec.Command("pg_ctl", "-D", data, "-m", "fast", "-w", "stop").CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_ctl stop slot %d: %v: %s", slot, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *DataStore) startRedis(slot int, c ServicesConfig) error {
	dir := d.redisDir(slot)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	pidFile := filepath.Join(dir, "redis.pid")
	// requirepass from services.conf: even loopback-only, an unauthenticated Redis lets any local
	// process read the cache. The password gates it, matching Postgres. The readiness ping below must
	// authenticate too (-a), so a wrong/missing password reads as not-ready, not silently open.
	cmd := exec.Command("redis-server",
		"--port", fmt.Sprint(c.Redis.Port),
		"--bind", "127.0.0.1",
		"--dir", dir,
		"--daemonize", "yes",
		"--pidfile", pidFile,
		"--requirepass", c.Redis.Password,
		"--save", "60", "1", // persist to the encrypted volume
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("redis start slot %d: %v: %s", slot, err, strings.TrimSpace(string(out)))
	}
	// brief readiness wait, authenticated
	for i := 0; i < 30; i++ {
		if exec.Command("redis-cli", "-p", fmt.Sprint(c.Redis.Port), "-a", c.Redis.Password, "ping").Run() == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("redis slot %d did not become ready", slot)
}

func (d *DataStore) stopRedis(slot int, c ServicesConfig) error {
	port := fmt.Sprint(c.Redis.Port)
	pw := c.Redis.Password
	if exec.Command("redis-cli", "-p", port, "-a", pw, "ping").Run() != nil {
		return nil // not running (or unreachable) , nothing to stop
	}
	out, err := exec.Command("redis-cli", "-p", port, "-a", pw, "shutdown", "nosave").CombinedOutput()
	// shutdown closes the connection, so an error here is often benign; check it actually stopped.
	if exec.Command("redis-cli", "-p", port, "-a", pw, "ping").Run() == nil {
		return fmt.Errorf("redis slot %d still up after shutdown: %s", slot, strings.TrimSpace(string(out)))
	}
	_ = err
	return nil
}
