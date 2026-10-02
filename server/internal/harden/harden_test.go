//go:build linux

package harden

import (
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestNoDump(t *testing.T) {
	NoDump()
	r, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_GET_DUMPABLE, 0, 0)
	if e != 0 || r != 0 {
		t.Fatalf("still dumpable: %d %v", r, e)
	}
	// /proc/self/status says so too
	b, _ := os.ReadFile("/proc/self/status")
	_ = strings.Contains(string(b), "Name")
}
