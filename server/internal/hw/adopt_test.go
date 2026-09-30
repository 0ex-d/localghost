package hw

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// The walk hands over what the old owner owns, chowns links without following them, leaves the
// root and the database directory for last, and never enters lost+found.
func TestChownOwnedWalk(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	data := filepath.Join(root, "postgres")
	must(os.MkdirAll(filepath.Join(data, "base"), 0o700))
	must(os.MkdirAll(filepath.Join(root, "frames", "2026"), 0o750))
	must(os.MkdirAll(filepath.Join(root, "lost+found"), 0o700))
	must(os.WriteFile(filepath.Join(root, "frames", "2026", "a.jpg"), []byte("x"), 0o640))
	must(os.WriteFile(filepath.Join(root, "lost+found", "#12"), []byte("x"), 0o600))
	must(os.WriteFile(filepath.Join(data, "PG_VERSION"), []byte("16"), 0o600))
	must(os.Symlink("/etc/hostname", filepath.Join(root, "frames", "link")))

	var got []string
	defer func(f func(string, int, int) error) { lchown = f }(lchown)
	lchown = func(p string, uid, gid int) error {
		got = append(got, strings.TrimPrefix(p, root+"/")+fmt.Sprintf(" %d:%d", uid, gid))
		return nil
	}
	me, mg := os.Getuid(), os.Getgid()
	n, failed := chownOwned(root, me, mg, 4242, 4343, data)
	if failed != 0 {
		t.Fatalf("failed %d", failed)
	}
	want := map[string]bool{
		"postgres/base 4242:4343": true, "postgres/PG_VERSION 4242:4343": true,
		"frames 4242:4343": true, "frames/2026 4242:4343": true, "frames/2026/a.jpg 4242:4343": true,
		"frames/link 4242:4343": true,
	}
	if n != len(want) || len(got) != len(want) {
		t.Fatalf("handed over %d: %v", n, got)
	}
	for _, g := range got {
		if !want[g] {
			t.Fatalf("unexpected chown %q (all: %v)", g, got)
		}
	}
	// someone else's files stay theirs
	if n, _ := chownOwned(root, me+1, mg, 4242, 4343); n != 0 {
		t.Fatalf("chowned %d files the old user does not own", n)
	}
}

func TestPreviousOwner(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "postgres")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	me := uint32(os.Getuid())
	if me == 0 {
		// as root both are root's: nothing to hand over
		if _, _, ok := previousOwner(root, data, 1234); ok {
			t.Fatal("root-owned volume taken for another user's")
		}
		return
	}
	if uid, _, ok := previousOwner(root, data, me+1); !ok || uid != me {
		t.Fatalf("previous owner %d %v", uid, ok)
	}
	if _, _, ok := previousOwner(root, data, me); ok {
		t.Fatal("already the run user's")
	}
}

func TestRunUserMustNotBeAnAppRole(t *testing.T) {
	c := ServicesConfig{}
	c.Postgres.User, c.Postgres.ROUser, c.Postgres.RWUser = "ghost", "ghost_ro", "ghost_rw"
	for _, bad := range []string{"ghost", "ghost_ro", "ghost_rw", "Ghostd", "ghost-d", ""} {
		if runUserFitsPostgres(bad, c) == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if err := runUserFitsPostgres("ghostd", c); err != nil {
		t.Fatal(err)
	}
}

// The whole hand-over against a real Postgres: initdb as one user, adopt to another, and the new
// user's peer login is a superuser. Needs root and two unprivileged users:
// GHOST_ADOPT_USERS=old,new (e.g. claude,ubuntu).
func TestAdoptVolumeEndToEnd(t *testing.T) {
	pair := strings.Split(os.Getenv("GHOST_ADOPT_USERS"), ",")
	if os.Geteuid() != 0 || len(pair) != 2 || osPgBin() == "" {
		t.Skip("needs root, Postgres and GHOST_ADOPT_USERS=old,new")
	}
	cred := func(name string) *syscall.Credential {
		u, err := user.Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		return &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	}
	oldC, newC := cred(pair[0]), cred(pair[1])
	base := t.TempDir()
	for p := base; p != "/" && p != os.TempDir(); p = filepath.Dir(p) {
		_ = os.Chmod(p, 0o711) // the old user must reach its volume
	}
	root := filepath.Join(base, "mnt", "slot0")
	data := filepath.Join(root, "postgres")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(data, 0o700))
	_ = os.Chmod(filepath.Join(base, "mnt"), 0o711)
	for _, p := range []string{root, data} {
		must(os.Chown(p, int(oldC.Uid), int(oldC.Gid)))
	}
	must(os.Chmod(root, 0o711))
	if out, err := pgCmdAs(root, oldC, "initdb", "-D", data, "--auth=trust").CombinedOutput(); err != nil {
		t.Fatalf("initdb: %v %s", err, out)
	}
	photo := filepath.Join(root, "frames", "a.jpg")
	must(os.MkdirAll(filepath.Dir(photo), 0o750))
	must(os.WriteFile(photo, []byte("x"), 0o640))
	must(os.Chown(filepath.Dir(photo), int(oldC.Uid), int(oldC.Gid)))
	must(os.Chown(photo, int(oldC.Uid), int(oldC.Gid)))
	secd := filepath.Join(root, "secd.json") // root's own file on the volume stays root's
	must(os.WriteFile(secd, []byte("{}"), 0o600))

	d := &DataStore{mountPathFor: func(int) string { return root }, runUser: pair[1]}
	c := ServicesConfig{}
	c.Postgres.User, c.Postgres.ROUser, c.Postgres.RWUser = "ghost", "ghost_ro", "ghost_rw"
	c.Redis.Port, c.Redis.Password = 1, "x" // nothing answers there
	// never under a live database: running as the old user, the hand-over refuses and touches nothing
	if out, err := pgCmdAs(root, oldC, "pg_ctl", "-D", data, "-l", filepath.Join(data, "log"), "-o",
		"-p 55498 -k "+data+" -c listen_addresses=", "-w", "start").CombinedOutput(); err != nil {
		t.Fatalf("start as the old user: %v %s", err, out)
	}
	if err := d.adoptVolume(0, c); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("adopted under a live database: %v", err)
	}
	if out, err := pgCmdAs(root, oldC, "pg_ctl", "-D", data, "-m", "fast", "-w", "stop").CombinedOutput(); err != nil {
		t.Fatalf("stop: %v %s", err, out)
	}
	must(d.adoptVolume(0, c))

	owner := func(p string) uint32 {
		fi, err := os.Lstat(p)
		must(err)
		return fi.Sys().(*syscall.Stat_t).Uid
	}
	for _, p := range []string{root, data, filepath.Join(data, "PG_VERSION"), filepath.Dir(photo), photo} {
		if owner(p) != newC.Uid {
			t.Fatalf("%s still owned by %d", p, owner(p))
		}
	}
	if owner(secd) != 0 {
		t.Fatal("root's file was handed over")
	}
	// the new user starts the database and its peer login is a superuser
	must(os.WriteFile(filepath.Join(data, "pg_hba.conf"), []byte("local all "+pair[1]+" peer\n"), 0o600))
	must(os.Chown(filepath.Join(data, "pg_hba.conf"), int(newC.Uid), int(newC.Gid)))
	port := "55499"
	if out, err := d.pgCmd(root, "pg_ctl", "-D", data, "-l", filepath.Join(data, "log"), "-o",
		"-p "+port+" -k "+data+" -c listen_addresses=", "-w", "start").CombinedOutput(); err != nil {
		t.Fatalf("start as the new user: %v %s", err, out)
	}
	defer d.pgCmd(root, "pg_ctl", "-D", data, "-m", "fast", "stop").Run()
	out, err := d.pgCmd(root, "psql", "-h", data, "-p", port, "-d", "postgres", "-tAc",
		"SELECT rolsuper FROM pg_roles WHERE rolname = current_user").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "t" {
		t.Fatalf("peer login as %s: %v %s", pair[1], err, out)
	}
	// once handed over, the next unlock does nothing
	if _, _, again := previousOwner(root, data, newC.Uid); again {
		t.Fatal("handed over twice")
	}
}
