//go:build unix

package pair

import (
	"os"
	"syscall"
	"unsafe"
)

// terminalCols returns the column count of the terminal on stdout, or 0 when stdout is not a
// terminal (piped, redirected) or the ioctl fails. Raw TIOCGWINSZ rather than golang.org/x/term:
// one syscall is not worth a dependency, and the box is always Linux.
func terminalCols() int {
	type winsize struct {
		rows, cols, xpixel, ypixel uint16
	}
	var ws winsize
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		os.Stdout.Fd(),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 {
		return 0
	}
	return int(ws.cols)
}
