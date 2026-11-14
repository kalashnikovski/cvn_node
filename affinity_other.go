//go:build !windows && !linux

package main

// setCurrentThreadCPU is not available on this system.
func setCurrentThreadCPU(cpu int) bool { return false }
