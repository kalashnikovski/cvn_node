//go:build windows

package node

import (
	"golang.org/x/sys/windows"
)

var procSetThreadAffinityMask = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadAffinityMask")

// setCurrentThreadCPU ties the calling operating-system thread to one
// processor thread. Machines with more than 64 processor threads are left
// alone: Windows splits those into groups this simple call does not cover.
func setCurrentThreadCPU(cpu int) bool {
	if cpu < 0 || cpu >= 64 {
		return false
	}
	previous, _, _ := procSetThreadAffinityMask.Call(uintptr(windows.CurrentThread()), uintptr(1)<<uint(cpu))
	return previous != 0
}
