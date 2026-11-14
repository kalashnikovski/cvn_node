//go:build !windows && !linux

package node

// setCurrentThreadCPU is not available on this system.
func setCurrentThreadCPU(cpu int) bool { return false }
