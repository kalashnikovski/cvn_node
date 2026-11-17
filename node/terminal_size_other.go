//go:build !windows && !linux

package node

// terminalRows is how many lines of text the terminal shows; not found out
// on this platform.
func terminalRows() int { return 0 }
