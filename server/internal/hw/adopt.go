package hw

// ADOPTION: the vault's files belong to the user the cohort runs as, and to nobody else. A box first
// set up with the cohort under a login account (coder, on xyntai) moves to a user of its own with
// tools/own_user.sh. Anything else running as the login account (a shell, an editor, a website on
// the same box) could read the decrypted volume through /proc/<pid>/root of any daemon; a user
// nobody logs in as closes that. The next cold unlock hands the volume over here, once, before
// Postgres starts:
//
//  1. a Postgres superuser named for the new user, made in single-user mode as the OLD owner (the
//     data directory is still theirs, and single-user mode needs no login). The peer line in
//     pg_hba then keeps a bootstrap identity (ensureOwnerAndDB) under the new name;
//  2. every file the old user owns on the volume, chowned to the new one (lchown: links are never
//     followed off the volume); files owned by anyone else (root's, secd's) are left as they are;
//  3. the Postgres data directory and the volume root last. The root's owner is what says "handed
//     over", so a walk cut short (power, a crash) is finished by the next unlock.

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// lchown is os.Lchown; tests record the calls instead.
var lchown = os.Lchown

// adoptVolume hands the slot's volume to the run user when another user owns it (see the top of
// this file). Nothing to do, cheaply, on every other unlock: two stats.
func (d *DataStore) adoptVolume(slot int, c ServicesConfig) error {
	cred := d.dbCredential()
	if cred == nil {
		return nil
	}
	root := d.mountPathFor(slot)
	data := d.pgData(slot)
	oldUID, oldGID, ok := previousOwner(root, data, cred.Uid)
	if !ok {
		return nil
	}
	if err := runUserFitsPostgres(d.runUser, c); err != nil {
		return err
	}
	old := &syscall.Credential{Uid: oldUID, Gid: oldGID}
	oldName := strconv.FormatUint(uint64(oldUID), 10)
	if u, err := user.LookupId(oldName); err == nil {
		oldName = u.Username
	}
	// Never under a live database: the box must have been locked (tools/own_user.sh checks too).
	if _, err := os.Stat(filepath.Join(data, "PG_VERSION")); err == nil {
		if pgCmdAs(root, old, "pg_ctl", "-D", data, "status").Run() == nil {
			return fmt.Errorf("Postgres is still running as %s; lock the box and unlock it again", oldName)
		}
	}
	if d.redisCli(root, c.Redis.Password, "-p", strconv.Itoa(c.Redis.Port), "ping").Run() == nil {
		return fmt.Errorf("Redis is still running as %s; lock the box and unlock it again", oldName)
	}
	t0 := time.Now()
	slog.Info("handing the volume to its own user", "fn", "adoptVolume", "slot", slot, "from", oldName, "to", d.runUser)
	if _, err := os.Stat(filepath.Join(data, "PG_VERSION")); err == nil {
		if err := superuserFor(root, data, old, d.runUser); err != nil {
			return fmt.Errorf("postgres superuser %s: %w", d.runUser, err)
		}
	}
	n, failed := chownOwned(root, int(oldUID), int(oldGID), int(cred.Uid), int(cred.Gid), data)
	if failed > 0 {
		// the root keeps its old owner, so the next unlock tries again; the unlock itself goes on
		// only if the database is already its own
		slog.Error("volume handed over only in part", "fn", "adoptVolume", "files", n, "failed", failed)
		return fmt.Errorf("%d files on the volume could not be handed to %s", failed, d.runUser)
	}
	for _, p := range []string{data, root} {
		if _, err := chownIfOwned(p, int(oldUID), int(oldGID), int(cred.Uid), int(cred.Gid)); err != nil {
			return fmt.Errorf("chown %s: %w", p, err)
		}
	}
	slog.Info("volume handed to its own user", "fn", "adoptVolume", "slot", slot, "to", d.runUser,
		"files", n, "took", time.Since(t0).Round(time.Millisecond).String())
	return nil
}

// previousOwner: the uid (and gid) that owns the volume when it is not the run user. The volume
// root says it (it is handed over last); a root owned by root (a volume from before the root was
// chowned) leaves it to the database directory. False when there is nothing to hand over.
func previousOwner(root, data string, runUID uint32) (uint32, uint32, bool) {
	for _, p := range []string{root, data} {
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || st.Uid == 0 {
			continue
		}
		if st.Uid == runUID {
			return 0, 0, false
		}
		return st.Uid, st.Gid, true
	}
	return 0, 0, false
}

// runUserFitsPostgres: the run user becomes a Postgres superuser of the same name, so it must be a
// plain identifier and must not be one of the app's own roles (a run user called ghost would make
// the database's owner a superuser).
func runUserFitsPostgres(name string, c ServicesConfig) error {
	if err := pgIdent(name); err != nil {
		return fmt.Errorf("run user %q: %w", name, err)
	}
	for _, r := range []string{c.Postgres.User, c.Postgres.ROUser, c.Postgres.RWUser} {
		if name == r {
			return fmt.Errorf("run user %q shares its name with a database role; give the cohort another user (tools/own_user.sh)", name)
		}
	}
	return nil
}

// superuserFor makes the Postgres superuser [name] in single-user mode, run as the data directory's
// current owner. Single-user mode takes no connections and needs no password: its session is the
// cluster's bootstrap superuser whatever it is called. Idempotent.
func superuserFor(mount, data string, as *syscall.Credential, name string) error {
	sql := fmt.Sprintf("DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = '%[1]s') "+
		"THEN CREATE ROLE %[1]s SUPERUSER LOGIN; ELSE ALTER ROLE %[1]s SUPERUSER LOGIN; END IF; END $$;\n", name)
	cmd := pgCmdAs(mount, as, "postgres", "--single", "-D", data, "postgres")
	cmd.Stdin = strings.NewReader(sql)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("%v: %s", err, text)
	}
	// single-user mode reports a failed statement and still exits 0 at the end of its input
	if strings.Contains(text, "ERROR:") || strings.Contains(text, "FATAL:") {
		return errors.New(text)
	}
	return nil
}

// chownOwned walks the volume and hands every entry owned by oldUID to newUID (the group follows
// when it was the old user's group). Links are chowned, never followed. The root and [last] are left
// for the caller to do at the end. lost+found is fsck's. Returns how many were handed over and how
// many could not be.
func chownOwned(root string, oldUID, oldGID, newUID, newGID int, last ...string) (n, failed int) {
	skip := map[string]bool{root: true}
	for _, p := range last {
		skip[filepath.Clean(p)] = true
	}
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			failed++
			slog.Warn("volume walk", "fn", "chownOwned", "path", p, "err", err)
			if e != nil && e.IsDir() && p != root {
				return fs.SkipDir
			}
			return nil
		}
		if e.IsDir() && p == filepath.Join(root, "lost+found") {
			return fs.SkipDir
		}
		if skip[p] {
			return nil
		}
		if changed, cerr := chownIfOwned(p, oldUID, oldGID, newUID, newGID); cerr != nil {
			failed++
			slog.Warn("volume file not handed over", "fn", "chownOwned", "path", p, "err", cerr)
		} else if changed {
			n++
		}
		return nil
	})
	return n, failed
}

// chownIfOwned hands one entry over when oldUID owns it (true); anything else is left alone, and is
// not an error.
func chownIfOwned(p string, oldUID, oldGID, newUID, newGID int) (bool, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return false, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != oldUID {
		return false, nil
	}
	gid := int(st.Gid)
	if gid == oldGID {
		gid = newGID
	}
	return true, lchown(p, newUID, gid)
}
