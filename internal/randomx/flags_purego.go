//go:build purego

package randomx

import (
	"runtime"
)

// GetFlags The recommended flags to be used on the current machine.
// Does not include:
// * RANDOMX_FLAG_LARGE_PAGES
// * RANDOMX_FLAG_FULL_MEM
// * RANDOMX_FLAG_SECURE
// These flags must be added manually if desired.
//
// On OpenBSD RANDOMX_FLAG_SECURE is enabled by default in JIT mode as W^X is enforced by the OS.
func GetFlags() (flags Flags) {
	flags = RANDOMX_FLAG_DEFAULT

	if runtime.GOOS == "openbsd" || runtime.GOOS == "netbsd" || ((runtime.GOOS == "darwin" || runtime.GOOS == "ios") && runtime.GOARCH == "arm64") {
		flags |= RANDOMX_FLAG_SECURE
	}

	return flags
}
