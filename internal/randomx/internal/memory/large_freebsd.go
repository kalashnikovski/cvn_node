//go:build freebsd && !purego

package memory

import (
	"golang.org/x/sys/unix"
)

type LargePageAllocator struct {
}

func NewLargePageAllocator() Allocator {
	return LargePageAllocator{}
}

/*
 * Request specific alignment (n == log2 of the desired alignment).
 *
 * MAP_ALIGNED_SUPER requests optimal superpage alignment, but does
 * not enforce a specific alignment.
 */
//#define	MAP_ALIGNED(n)	 ((n) << MAP_ALIGNMENT_SHIFT)
//#define	MAP_ALIGNMENT_SHIFT	24
//#define	MAP_ALIGNMENT_MASK	MAP_ALIGNED(0xff)
//#define	MAP_ALIGNED_SUPER	MAP_ALIGNED(1) /* align on a superpage */

const MAP_ALIGNED_SUPER = 1 << 24

func (a LargePageAllocator) AllocMemory(size uint64) ([]byte, error) {

	memory, err := unix.Mmap(-1, 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS|MAP_ALIGNED_SUPER)
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
