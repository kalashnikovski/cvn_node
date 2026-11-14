//go:build linux

package node

import (
	"golang.org/x/sys/unix"
)

// setCurrentThreadCPU ties the calling operating-system thread to one
// processor thread.
func setCurrentThreadCPU(cpu int) bool {
	var set unix.CPUSet
	set.Set(cpu)
	return unix.SchedSetaffinity(0, &set) == nil
}
