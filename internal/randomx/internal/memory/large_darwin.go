//go:build darwin && amd64 && !purego

package memory

import (
	"golang.org/x/sys/unix"
)

type LargePageAllocator struct {
}

func NewLargePageAllocator() Allocator {
	return LargePageAllocator{}
}

//#define VM_FLAGS_SUPERPAGE_SHIFT 16
//#define SUPERPAGE_NONE                  0       /* no superpages, if all bits are 0 */
//#define SUPERPAGE_SIZE_ANY              1
//#define VM_FLAGS_SUPERPAGE_NONE     (SUPERPAGE_NONE     << VM_FLAGS_SUPERPAGE_SHIFT)
//#define VM_FLAGS_SUPERPAGE_SIZE_ANY (SUPERPAGE_SIZE_ANY << VM_FLAGS_SUPERPAGE_SHIFT)
//#define SUPERPAGE_SIZE_2MB              2
//#define VM_FLAGS_SUPERPAGE_SIZE_2MB (SUPERPAGE_SIZE_2MB<<VM_FLAGS_SUPERPAGE_SHIFT)

const VM_FLAGS_SUPERPAGE_SIZE_ANY = 1 << 16
const VM_FLAGS_SUPERPAGE_SIZE_2MB = 2 << 16

func (a LargePageAllocator) AllocMemory(size uint64) ([]byte, error) {

	memory, err := unix.Mmap(VM_FLAGS_SUPERPAGE_SIZE_2MB, 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
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
