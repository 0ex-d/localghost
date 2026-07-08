package hw

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/searchsql"
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
	if err := pgIdent(c.Postgres.User); err != nil {
		return fmt.Errorf("services.conf postgres user: %w", err)
	}
	if err := pgIdent(c.Postgres.Name); err != nil {
		return fmt.Errorf("services.conf postgres db name: %w", err)
	}
	stmts := []string{
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD %s;", c.Postgres.User, pgLit(c.Postgres.Password)),
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
-- frames: ghost.framed's photo archive index. hash is the identity (sha256/128 of the raw bytes),
-- archive_path holds the untouched original, preview/thumb are derived JPEGs. taken_at from EXIF
-- DateTimeOriginal, falling back to upload mtime. Re-inserts are ON CONFLICT DO NOTHING , idempotent.
CREATE TABLE IF NOT EXISTS frames (
  hash         TEXT PRIMARY KEY,
  taken_at     BIGINT NOT NULL DEFAULT 0,
  lat          DOUBLE PRECISION NOT NULL DEFAULT 0,
  lon          DOUBLE PRECISION NOT NULL DEFAULT 0,
  has_gps      BOOLEAN NOT NULL DEFAULT FALSE,
  archive_path TEXT NOT NULL,
  preview_path TEXT NOT NULL DEFAULT '',
  thumb_path   TEXT NOT NULL DEFAULT '',
  bytes        BIGINT NOT NULL DEFAULT 0,
  source       TEXT NOT NULL DEFAULT '',
  received_at  BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS frames_taken_at ON frames (taken_at);
-- location_points: watch/phone position samples, the raw material for the daily GeoJSON path. The
-- box stores coordinates and NEVER contacts a map/tile service; rendering is the phone's job.
CREATE TABLE IF NOT EXISTS location_points (
  ts     BIGINT NOT NULL,
  lat    DOUBLE PRECISION NOT NULL,
  lon    DOUBLE PRECISION NOT NULL,
  source TEXT NOT NULL DEFAULT 'watch',
  PRIMARY KEY (ts, source)
);
`
	if out, err := exec.Command("psql", "-h", data, "-p", port, "-d", c.Postgres.Name, "-v", "ON_ERROR_STOP=1", "-c", schema).CombinedOutput(); err != nil {
		return fmt.Errorf("apply schema: %v: %s", err, strings.TrimSpace(string(out)))
	}

	// Role split. ghost_ro can only SELECT; ghost_rw can write the app tables. Daemons connect as one
	// of these, never as the owner. Grants cover existing tables plus a DEFAULT PRIVILEGES rule so
	// tables added by later migrations inherit the same access without re-granting. ghost_rw also needs
	// USAGE on sequences (BIGSERIAL id) to INSERT.
	for _, ident := range []string{c.Postgres.ROUser, c.Postgres.RWUser} {
		if err := pgIdent(ident); err != nil {
			return fmt.Errorf("services.conf service role: %w", err)
		}
	}
	roleSQL := fmt.Sprintf(`
CREATE ROLE %[1]s LOGIN PASSWORD %[2]s;
CREATE ROLE %[3]s LOGIN PASSWORD %[4]s;
GRANT CONNECT ON DATABASE %[5]s TO %[1]s, %[3]s;
GRANT USAGE ON SCHEMA public TO %[1]s, %[3]s;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO %[1]s;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %[3]s;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %[3]s;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO %[1]s;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %[3]s;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO %[3]s;`,
		c.Postgres.ROUser, pgLit(c.Postgres.ROPass), c.Postgres.RWUser, pgLit(c.Postgres.RWPass), c.Postgres.Name)
	if out, err := exec.Command("psql", "-h", data, "-p", port, "-d", c.Postgres.Name, "-v", "ON_ERROR_STOP=1", "-c", roleSQL).CombinedOutput(); err != nil {
		return fmt.Errorf("create roles: %v: %s", err, strings.TrimSpace(string(out)))
	}

	// Search layer schema (SPEC v1.1), applied as the OWNER over the still-trust socket (the
	// migrations-run-as-owner rule). Order matters: this must run BEFORE pg_hba hardening, because the
	// owner role authenticates by trust only. pgvector is optional , absence is the documented FTS-only
	// degraded mode, recorded in search.meta so ghost.searchd reports it honestly.
	if err := d.applySearchSchema(data, port, c); err != nil {
		return fmt.Errorf("apply search schema: %w", err)
	}

	// Harden pg_hba: switch local socket auth from trust to scram-sha-256, so a role's password is
	// actually verified (the whole point of the split , without this, ghost_ro could log in as
	// ghost_rw). initdb wrote passwords as scram already (default password_encryption). Reload for the
	// new pg_hba to take effect; the owner connection above used trust, which still worked pre-reload.
	if err := d.hardenPgHBA(slot, c); err != nil {
		return fmt.Errorf("harden pg_hba: %w", err)
	}

	// No separate credential file , services.conf (written at provision) is the single credential
	// store, read by DataStore and anything else that needs to connect. The password was applied to
	// the role above; ghost.secd reads it from services.conf to connect over TCP.
	return nil
}

// hardenPgHBA rewrites pg_hba.conf so local and loopback connections use scram-sha-256 instead of the
// initdb trust default, then reloads Postgres. After this, a password is actually checked , which is
// what makes the ghost_ro / ghost_rw split real: ghost_ro cannot present ghost_rw's password it does
// not have. The socket is still loopback/volume-local; scram is defence in depth on top of that, not
// a replacement for it.
func (d *DataStore) hardenPgHBA(slot int, c ServicesConfig) error {
	data := d.pgData(slot)
	hba := "# ghost: scram-sha-256 on the volume-local socket and loopback. Rewritten at first start.\n" +
		"local   all   all                  scram-sha-256\n" +
		"host    all   all   127.0.0.1/32   scram-sha-256\n" +
		"host    all   all   ::1/128        scram-sha-256\n"
	if err := os.WriteFile(filepath.Join(data, "pg_hba.conf"), []byte(hba), 0o600); err != nil {
		return err
	}
	out, err := exec.Command("pg_ctl", "-D", data, "reload").CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_ctl reload: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// pgIdent admits only strict lower-case identifiers for role/database names sourced from
// services.conf. services.conf is DATA , generated by us, but readable and theoretically editable on
// disk , and these names are spliced into owner-privilege SQL where quoting rules differ from string
// literals. A name outside [a-z_][a-z0-9_]* means the conf was tampered with or corrupted; refusing
// loudly beats accepting quietly.
func pgIdent(name string) error {
	if name == "" {
		return fmt.Errorf("empty identifier")
	}
	for i, c := range name {
		ok := c == '_' || (c >= 'a' && c <= 'z') || (i > 0 && c >= '0' && c <= '9')
		if !ok {
			return fmt.Errorf("identifier %q outside [a-z_][a-z0-9_]*", name)
		}
	}
	return nil
}

// pgLit renders a string as a single-quoted SQL literal with quotes doubled. The passwords this wraps
// are randHex by construction, so the escape is normally a no-op , it exists so a hand-edited
// services.conf cannot turn provisioning into superuser SQL injection.
func pgLit(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// applySearchSchema applies the search layer DDL (searchsql) as the owner. Core (FTS) must succeed;
// the vector add-on is best-effort , if the pgvector extension is unavailable, the FTS-only schema and
// reduced health view apply and search.meta records vector=off. Grants run last so they cover
// everything just created.
func (d *DataStore) applySearchSchema(sockDir, port string, c ServicesConfig) error {
	run := func(sql string) error {
		out, err := exec.Command("psql", "-h", sockDir, "-p", port, "-d", c.Postgres.Name,
			"-v", "ON_ERROR_STOP=1", "-c", sql).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := run(searchsql.SchemaCore); err != nil {
		return fmt.Errorf("core: %w", err)
	}
	if err := run(searchsql.SchemaVector); err != nil {
		// pgvector missing (not installed on the box). FTS-only degraded mode, stated, not fatal.
		if e2 := run(searchsql.SchemaNoVector + searchsql.HealthViewNoVector); e2 != nil {
			return fmt.Errorf("no-vector fallback: %w", e2)
		}
	} else if err := run(searchsql.HealthView); err != nil {
		return fmt.Errorf("health view: %w", err)
	}
	if err := run(searchsql.Grants); err != nil {
		return fmt.Errorf("grants: %w", err)
	}
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
			return d.ensureRedisACL(slot, c)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("redis slot %d did not become ready", slot)
}

// ensureRedisACL defines the two service users: ghost_ro (+@read, all keys) and ghost_rw (+@read
// +@write +@keyspace on all keys). The daemons authenticate as one of these; the default user's
// password stays for admin/readiness only. Idempotent , ACL SETUSER overwrites. Run on every start
// (cheap) so a restart re-asserts the ACL even if it was cleared.
func (d *DataStore) ensureRedisACL(slot int, c ServicesConfig) error {
	if c.Redis.ROUser == "" || c.Redis.RWUser == "" {
		return nil // pre-role config; nothing to assert
	}
	users := [][]string{
		{"ACL", "SETUSER", c.Redis.ROUser, "on", ">" + c.Redis.ROPass, "~*", "+@read", "+ping", "+auth", "+hello"},
		{"ACL", "SETUSER", c.Redis.RWUser, "on", ">" + c.Redis.RWPass, "~*", "+@read", "+@write", "+@keyspace", "+ping", "+auth", "+hello"},
	}
	for _, u := range users {
		args := append([]string{"-p", fmt.Sprint(c.Redis.Port), "-a", c.Redis.Password}, u...)
		if out, err := exec.Command("redis-cli", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("redis acl %s: %v: %s", u[2], err, strings.TrimSpace(string(out)))
		}
	}
	return nil
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
