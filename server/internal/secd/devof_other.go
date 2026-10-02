//go:build !linux && !windows

package secd

import (
	"os"
	"syscall"
)

// devOf is the device a file lives on, from its stat (Darwin and the BSDs keep it in Dev as
// well, as a signed 32-bit number). A box is Linux; this keeps the tree building elsewhere.
func devOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev)
	}
	return 0
}
