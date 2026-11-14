//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

// availableMemoryBytes reports how much physical memory is free for
// programs to use right now.
func availableMemoryBytes() (uint64, bool) {
	var status memoryStatusEx
	status.length = uint32(unsafe.Sizeof(status))
	if result, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status))); result == 0 {
		return 0, false
	}
	return status.availPhys, true
}
