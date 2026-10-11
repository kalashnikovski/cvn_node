//go:build windows
// +build windows

package main

import (
	"syscall"
)

// This init block runs automatically BEFORE func main(), but ONLY when compiling on Windows!
func init() {
	// Target the exact Windows system binary "kernel32.dll" securely
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	if kernel32 != nil {
		setConsoleModeProc := kernel32.NewProc("SetConsoleMode")
		getStdHandleProc := kernel32.NewProc("GetStdHandle")
		
		// STD_OUTPUT_HANDLE identifier is natively 11 on Windows kernels
		handle, _, _ := getStdHandleProc.Call(uintptr(11))
		var mode uint32 = 0x0004 // ENABLE_VIRTUAL_TERMINAL_PROCESSING
		
		_, _, _ = setConsoleModeProc.Call(handle, uintptr(mode))
	}
}