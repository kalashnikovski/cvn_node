//go:build !windows && !linux

package node

// availableMemoryBytes is not known on this system.
func availableMemoryBytes() (uint64, bool) { return 0, false }
