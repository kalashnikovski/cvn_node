//go:build windows
// +build windows

package main

import (
	"syscall"
)

// This init block runs automatically BEFORE func main() on ALL Windows build targets!
func init() {
	// Target the exact Windows system binary "kernel32.dll" securely
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	if kernel32 != nil {
		setConsoleModeProc := kernel32.NewProc("SetConsoleMode")
		getStdHandleProc := kernel32.NewProc("GetStdHandle")
		
		// STD_OUTPUT_HANDLE identifier is natively 11 on Windows kernels
		handle, _, _ := getStdHandleProc.Call(uintptr(11))
		
		// 0x0001 (ENABLE_PROCESSED_OUTPUT) | 0x0004 (ENABLE_VIRTUAL_TERMINAL_PROCESSING)
		var mode uint32 = 0x0001 | 0x0004 
		
		_, _, _ = setConsoleModeProc.Call(handle, uintptr(mode))
	}
}