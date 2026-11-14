//go:build windows && !purego

package memory

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var PageNoMemoryErr = errors.New("error allocating page")

type PageAllocator struct {
}

func NewPageAllocator() Allocator {
	return PageAllocator{}
}

func (a PageAllocator) AllocMemory(size uint64) ([]byte, error) {
	memory, err := windows.VirtualAlloc(0, uintptr(size), windows.MEM_COMMIT, windows.PAGE_READWRITE)
	if err != nil {
		return nil, errors.Join(PageNoMemoryErr, err)
	}

	return unsafe.Slice((*byte)(unsafe.Pointer(memory)), size), nil
}

func (a PageAllocator) FreeMemory(memory []byte) error {
	if memory == nil {
		return nil
	}

	return windows.VirtualFree(uintptr(unsafe.Pointer(unsafe.SliceData(memory))), 0, windows.MEM_RELEASE)
}

func PageReadWrite(memory []byte) error {
	var oldProtect uint32
	return windows.VirtualProtect(uintptr(unsafe.Pointer(unsafe.SliceData(memory))), uintptr(len(memory)), windows.PAGE_READWRITE, &oldProtect)
}

func PageReadExecute(memory []byte) error {
	var oldProtect uint32
	return windows.VirtualProtect(uintptr(unsafe.Pointer(unsafe.SliceData(memory))), uintptr(len(memory)), windows.PAGE_EXECUTE_READ, &oldProtect)
}

// PageReadWriteExecute Insecure!
func PageReadWriteExecute(memory []byte) error {
	var oldProtect uint32
	return windows.VirtualProtect(uintptr(unsafe.Pointer(unsafe.SliceData(memory))), uintptr(len(memory)), windows.PAGE_EXECUTE_READWRITE, &oldProtect)
}
