//go:build !linux

package gpu

// readKernelLog has nothing to read outside Linux (no syslog(2), no GPU driver, no box).
func readKernelLog() string { return "" }
