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

// ports are derived from the slot so the three accounts never collide. Loopback only.
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
	if err := d.startPostgres(slot); err != nil {
		return Endpoints{}, err
	}
	if err := d.startRedis(slot); err != nil {
		// roll back postgres so we do not leave a half-started account
		_ = d.stopPostgres(slot)
		return Endpoints{}, err
	}
	return Endpoints{
		PostgresPort: pgPort(slot),
		RedisPort:    redisPort(slot),
		Socket:       d.pgData(slot),
	}, nil
}

// Stop tears both down on lock/unmount, so nothing holds the volume open when we close it.
func (d *DataStore) Stop(slot int) error {
	rerr := d.stopRedis(slot)
	perr := d.stopPostgres(slot)
	if perr != nil {
		return perr
	}
	return rerr
}

func (d *DataStore) startPostgres(slot int) error {
	data := d.pgData(slot)
	// initdb on first run (the data dir lives in the encrypted volume).
	if _, err := os.Stat(filepath.Join(data, "PG_VERSION")); os.IsNotExist(err) {
		if err := os.MkdirAll(data, 0o700); err != nil {
			return err
		}
		if out, err := exec.Command("initdb", "-D", data, "--auth=trust", "--encoding=UTF8").CombinedOutput(); err != nil {
			return fmt.Errorf("initdb slot %d: %v: %s", slot, err, strings.TrimSpace(string(out)))
		}
	}
	// start, bound to loopback on the slot's port, socket inside the volume.
	opts := fmt.Sprintf("-p %d -k %s -c listen_addresses=127.0.0.1", pgPort(slot), data)
	cmd := exec.Command("pg_ctl", "-D", data, "-o", opts, "-w", "-t", "30", "start")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pg_ctl start slot %d: %v: %s", slot, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *DataStore) stopPostgres(slot int) error {
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

func (d *DataStore) startRedis(slot int) error {
	dir := d.redisDir(slot)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	pidFile := filepath.Join(dir, "redis.pid")
	cmd := exec.Command("redis-server",
		"--port", fmt.Sprint(redisPort(slot)),
		"--bind", "127.0.0.1",
		"--dir", dir,
		"--daemonize", "yes",
		"--pidfile", pidFile,
		"--save", "60", "1", // persist to the encrypted volume
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("redis start slot %d: %v: %s", slot, err, strings.TrimSpace(string(out)))
	}
	// brief readiness wait
	for i := 0; i < 30; i++ {
		if exec.Command("redis-cli", "-p", fmt.Sprint(redisPort(slot)), "ping").Run() == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("redis slot %d did not become ready", slot)
}

func (d *DataStore) stopRedis(slot int) error {
	if exec.Command("redis-cli", "-p", fmt.Sprint(redisPort(slot)), "ping").Run() != nil {
		return nil // not running
	}
	out, err := exec.Command("redis-cli", "-p", fmt.Sprint(redisPort(slot)), "shutdown", "nosave").CombinedOutput()
	// shutdown closes the connection, so an error here is often benign; check it actually stopped.
	if exec.Command("redis-cli", "-p", fmt.Sprint(redisPort(slot)), "ping").Run() == nil {
		return fmt.Errorf("redis slot %d still up after shutdown: %s", slot, strings.TrimSpace(string(out)))
	}
	_ = err
	return nil
}
