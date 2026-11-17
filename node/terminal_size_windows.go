//go:build windows

package node

import (
	"syscall"
	"unsafe"
)

// terminalRows is how many lines of text the console window shows, or 0 if
// that cannot be found out (output is not a console).
func terminalRows() int {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getStdHandle := kernel32.NewProc("GetStdHandle")
	getInfo := kernel32.NewProc("GetConsoleScreenBufferInfo")
	// STD_OUTPUT_HANDLE is -11.
	handle, _, _ := getStdHandle.Call(^uintptr(10))
	// CONSOLE_SCREEN_BUFFER_INFO: eleven 16-bit fields. The window's top and
	// bottom rows are the seventh and ninth.
	var info [11]int16
	ok, _, _ := getInfo.Call(handle, uintptr(unsafe.Pointer(&info[0])))
	if ok == 0 {
		return 0
	}
	top, bottom := int(info[6]), int(info[8])
	if bottom < top {
		return 0
	}
	return bottom - top + 1
}
