package memory

import "unsafe"

type AlignedAllocator uint64

func NewAlignedAllocator(alignment uint64) Allocator {
	if !isZeroOrPowerOf2(alignment) {
		panic("alignment must be a power of 2")
	}
	return AlignedAllocator(alignment)
}

func (a AlignedAllocator) AllocMemory(size uint64) ([]byte, error) {
	if a <= 4 {
		//slice allocations are 16-byte aligned, fast path
		return make([]byte, size, max(size, uint64(a))), nil
	}

	memory := make([]byte, size+uint64(a))
	ptr := uintptr(unsafe.Pointer(unsafe.SliceData(memory)))
	align := uint64(a) - (uint64(ptr) & (uint64(a) - 1))
	if align == uint64(a) {
		return memory[:size:size], nil
	}
	return memory[align : align+size : align+size], nil
}

func (a AlignedAllocator) FreeMemory(memory []byte) error {
	//let gc free
	return nil
}
