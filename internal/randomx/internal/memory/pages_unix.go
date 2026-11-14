//go:build unix && !linux && !netbsd && !purego

package memory

import (
	"golang.org/x/sys/unix"
)

var PageNoMemoryErr = unix.ENOMEM

type PageAllocator struct {
}

func NewPageAllocator() Allocator {
	return PageAllocator{}
}

func (a PageAllocator) AllocMemory(size uint64) ([]byte, error) {
	memory, err := unix.Mmap(-1, 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err != nil {
		return nil, err
	}

	return memory, nil
}

func (a PageAllocator) FreeMemory(memory []byte) error {
	if memory == nil {
		return nil
	}

	return unix.Munmap(memory)
}

func PageReadWrite(memory []byte) error {
	return unix.Mprotect(memory, unix.PROT_READ|unix.PROT_WRITE)
}

func PageReadExecute(memory []byte) error {
	return unix.Mprotect(memory, unix.PROT_READ|unix.PROT_EXEC)
}

// PageReadWriteExecute Insecure!
func PageReadWriteExecute(memory []byte) error {
	return unix.Mprotect(memory, unix.PROT_READ|unix.PROT_WRITE|unix.PROT_EXEC)
}
