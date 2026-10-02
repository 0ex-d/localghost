//go:build linux

package gpu

import "syscall"

// readKernelLog reads the kernel ring buffer through syslog(2): the size first, then all of it.
func readKernelLog() string {
	n, err := syscall.Klogctl(10, nil) // SYSLOG_ACTION_SIZE_BUFFER
	if err != nil || n <= 0 {
		return ""
	}
	if n > 16<<20 {
		n = 16 << 20
	}
	buf := make([]byte, n)
	m, err := syscall.Klogctl(3, buf) // SYSLOG_ACTION_READ_ALL
	if err != nil || m <= 0 {
		return ""
	}
	return string(buf[:m])
}
