//go:build linux

package procs

import "syscall"

// ChildAttr is how a daemon starts a child it must never outlive it: its own process group, so the
// parent can signal the whole group on a graceful stop, and Pdeathsig, so the KERNEL kills the
// child the moment its parent dies (a SIGKILLed parent cannot clean up after itself; the kernel
// can). The orphan systemd once caught held port 18080 and 9 GB of VRAM for an hour. Pdeathsig is
// Linux's; attr_other.go is the same without it, so the tree builds and its tests run on a Mac
// (where no box runs, but a contributor's editor does).
func ChildAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}
