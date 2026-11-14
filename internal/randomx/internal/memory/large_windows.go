//go:build windows && !purego

package memory

// Large pages on Windows. This file is a CVN addition to the engine; see
// README_CVN.md in the randomx directory.
//
// Windows hands out memory in large (2 MiB) pages only to a process that
// holds the "Lock pages in memory" right and has switched it on. The right
// is granted per user account by an administrator (Local Security Policy,
// User Rights Assignment) and takes effect at the next sign-in. Without it,
// allocation here fails and the caller falls back to ordinary memory.

import (
	"errors"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var LargePageNoMemoryErr = errors.New("error allocating large page")

const memLargePages = 0x20000000

var (
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procGetLargePageMinimum = kernel32.NewProc("GetLargePageMinimum")

	lockMemoryOnce sync.Once
)

type LargePageAllocator struct {
}

func NewLargePageAllocator() Allocator {
	return LargePageAllocator{}
}

// enableLockMemoryPrivilege switches on the "Lock pages in memory" right for
// this process, if the account has it. Whether it worked shows when memory is
// requested.
func enableLockMemoryPrivilege() {
	var token windows.Token
	if windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token) != nil {
		return
	}
	defer token.Close()
	name, err := windows.UTF16PtrFromString("SeLockMemoryPrivilege")
	if err != nil {
		return
	}
	var luid windows.LUID
	if windows.LookupPrivilegeValue(nil, name, &luid) != nil {
		return
	}
	privileges := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges:     [1]windows.LUIDAndAttributes{{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}},
	}
	_ = windows.AdjustTokenPrivileges(token, false, &privileges, 0, nil, nil)
}

func largePageSize() uint64 {
	size, _, _ := procGetLargePageMinimum.Call()
	if size == 0 {
		return 0 // large pages are not available on this system
	}
	return uint64(size)
}

func (a LargePageAllocator) AllocMemory(size uint64) ([]byte, error) {
	lockMemoryOnce.Do(enableLockMemoryPrivilege)
	pageSize := largePageSize()
	if pageSize == 0 || size == 0 {
		return nil, LargePageNoMemoryErr
	}
	// A large-page request must be a whole number of large pages.
	rounded := (size + pageSize - 1) / pageSize * pageSize
	memory, err := windows.VirtualAlloc(0, uintptr(rounded), windows.MEM_COMMIT|windows.MEM_RESERVE|memLargePages, windows.PAGE_READWRITE)
	if err != nil {
		return nil, errors.Join(LargePageNoMemoryErr, err)
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(memory)), size), nil
}

func (a LargePageAllocator) FreeMemory(memory []byte) error {
	if memory == nil {
		return nil
	}
	return windows.VirtualFree(uintptr(unsafe.Pointer(unsafe.SliceData(memory))), 0, windows.MEM_RELEASE)
}
