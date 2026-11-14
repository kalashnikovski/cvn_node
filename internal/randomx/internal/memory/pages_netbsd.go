//go:build netbsd && !purego

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

func _netbsd_PROT_MPROTECT(prot int) {
	// PROT_MPROTECT(x) is defined as ((x) << 3):
	// https://github.com/NetBSD/src/blob/aba449a55bf91b44bc68f542edd9afa341962b89/sys/sys/mman.h#L73
	return prot << 3
}

func (a PageAllocator) AllocMemory(size uint64) ([]byte, error) {
	memory, err := unix.Mmap(-1, 0, int(size), unix.PROT_READ|unix.PROT_WRITE|_netbsd_PROT_MPROTECT(unix.PROT_EXEC), unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
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
