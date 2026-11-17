//go:build linux

package node

import (
	"os"
	"syscall"
	"unsafe"
)

// terminalRows is how many lines of text the terminal shows, or 0 if that
// cannot be found out (output is not a terminal).
func terminalRows() int {
	var size struct{ rows, cols, xPixels, yPixels uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0
	}
	return int(size.rows)
}
