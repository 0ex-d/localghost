package hw

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The Redis passwords stay off every command line: the server reads requirepass from a 0600 file,
// redis-cli gets it from REDISCLI_AUTH, and ACL SETUSER (with the service passwords) goes in on
// stdin. Runs against a real redis-server when one is installed.
func TestRedisPasswordsOffTheCommandLine(t *testing.T) {
	if _, err := exec.LookPath("redis-server"); err != nil {
		t.Skip("no redis-server")
	}
	dir := t.TempDir()
	pw := `p"w\ 1` // quotes and a backslash survive the quoting
	conf := filepath.Join(dir, "auth.conf")
	if err := os.WriteFile(conf, []byte("requirepass "+redisQuote(pw)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	port := 56379
	srv := exec.Command("redis-server", conf, "--port", fmt.Sprint(port), "--bind", "127.0.0.1", "--dir", dir, "--save", "")
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Process.Kill(); _ = srv.Wait() }()
	// no password anywhere in the server's argv
	b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", srv.Process.Pid))
	if strings.Contains(string(b), "p\"w") {
		t.Fatalf("password in argv: %q", b)
	}
	d := &DataStore{}
	var err error
	for i := 0; i < 50; i++ {
		if err = d.redisCli(dir, pw, "-p", fmt.Sprint(port), "ping").Run(); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("authenticated ping: %v", err)
	}
	// wrong password: refused
	if out, _ := d.redisCli(dir, "nope", "-p", fmt.Sprint(port), "ping").CombinedOutput(); strings.Contains(string(out), "PONG") {
		t.Fatal("a wrong password answered")
	}
	// ACL over stdin, then that user can log in
	cmd := d.redisCli(dir, pw, "-p", fmt.Sprint(port))
	cmd.Stdin = strings.NewReader(redisLine([]string{"ACL", "SETUSER", "ghost_ro", "on", ">ro pass\"x", "~*", "+@read", "+ping"}))
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "OK") {
		t.Fatalf("acl over stdin: %v %s", err, out)
	}
	c2 := exec.Command("redis-cli", "-p", fmt.Sprint(port), "--user", "ghost_ro", "--pass", `ro pass"x`, "ping")
	if out, _ := c2.CombinedOutput(); !strings.Contains(string(out), "PONG") {
		t.Fatalf("the ACL user cannot log in: %s", out)
	}
}
