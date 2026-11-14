package memory

import (
	"unsafe"
)

type Allocator interface {
	AllocMemory(size uint64) ([]byte, error)
	FreeMemory(memory []byte) error
}

func Allocate[T any](a Allocator) (*T, error) {
	var zeroType T

	mem, err := a.AllocMemory(uint64(unsafe.Sizeof(zeroType)))
	if err != nil {
		return nil, err
	}
	return (*T)(unsafe.Pointer(unsafe.SliceData(mem))), nil
}

func Free[T any](a Allocator, v *T) error {
	return a.FreeMemory(unsafe.Slice((*byte)(unsafe.Pointer(v)), uint64(unsafe.Sizeof(*v))))
}

func AllocateSlice[T any, T2 ~int | ~uint64 | ~uint32](a Allocator, size T2) ([]T, error) {
	var zeroType T

	mem, err := a.AllocMemory(uint64(unsafe.Sizeof(zeroType)) * uint64(size))
	if err != nil {
		return nil, err
	}
	return unsafe.Slice((*T)(unsafe.Pointer(unsafe.SliceData(mem))), size), nil
}

func FreeSlice[T any](a Allocator, v []T) error {
	var zeroType T

	return a.FreeMemory(unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(v))), uint64(unsafe.Sizeof(zeroType))*uint64(len(v))))
}

func isZeroOrPowerOf2(x uint64) bool {
	return (x & (x - 1)) == 0
}
