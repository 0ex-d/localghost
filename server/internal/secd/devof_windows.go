//go:build windows

package secd

import "os"

// devOf has no device number to read on Windows; nothing here is a mount point.
func devOf(fi os.FileInfo) uint64 { return 0 }
