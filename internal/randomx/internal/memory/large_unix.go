//go:build unix && !(freebsd || openbsd || netbsd || dragonfly || darwin || ios) && !purego

package memory

import (
	"golang.org/x/sys/unix"
)

type LargePageAllocator struct {
}

func NewLargePageAllocator() Allocator {
	return LargePageAllocator{}
}

func (a LargePageAllocator) AllocMemory(size uint64) ([]byte, error) {
	memory, err := unix.Mmap(-1, 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS|unix.MAP_HUGETLB|unix.MAP_POPULATE)
	if err != nil {
		return nil, err
	}

	return memory, nil
}

func (a LargePageAllocator) FreeMemory(memory []byte) error {
	if memory == nil {
		return nil
	}

	return unix.Munmap(memory)
}
