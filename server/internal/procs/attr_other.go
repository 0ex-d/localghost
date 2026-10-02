//go:build !linux

package procs

import "syscall"

// ChildAttr without Pdeathsig, which only Linux has: the process group alone. A box is Linux; this
// is for building and testing the tree elsewhere.
func ChildAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
