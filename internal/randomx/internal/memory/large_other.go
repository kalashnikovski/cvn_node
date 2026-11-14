//go:build openbsd || netbsd || dragonfly || (darwin && !amd64) || ios || (!unix && !windows) || purego

package memory

var LargePageNoMemoryErr error

// NewLargePageAllocator Not supported in platform
func NewLargePageAllocator() Allocator {
	return nil
}
