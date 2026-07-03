//go:build !unix

package pair

// terminalCols on non-unix platforms: no ioctl, report unknown. TerminalCols falls back to the
// COLUMNS environment variable and then to 80. The box itself is always Linux; this exists only so
// the package builds and tests on a Windows or other dev machine.
func terminalCols() int { return 0 }
