package randomx

import "errors"

type Flags uint64

func (f Flags) Has(flags Flags) bool {
	return f&flags == flags
}

//go:nosplit
func (f Flags) V2() bool {
	return f.Has(RANDOMX_FLAG_V2)
}

//go:nosplit
func (f Flags) HasJIT() bool {
	return f.Has(RANDOMX_FLAG_JIT) && supportsJIT
}

const RANDOMX_FLAG_DEFAULT Flags = 0

const (
	// RANDOMX_FLAG_LARGE_PAGES Select large page allocation for dataset
	RANDOMX_FLAG_LARGE_PAGES = Flags(1 << iota)
	// RANDOMX_FLAG_HARD_AES Selects between hardware or software AES
	RANDOMX_FLAG_HARD_AES
	// RANDOMX_FLAG_FULL_MEM Selects between full or light mode dataset
	RANDOMX_FLAG_FULL_MEM
	// RANDOMX_FLAG_JIT Enables JIT features
	RANDOMX_FLAG_JIT
	// RANDOMX_FLAG_SECURE Enables W^X for JIT code
	RANDOMX_FLAG_SECURE
	RANDOMX_FLAG_ARGON2_SSSE3
	RANDOMX_FLAG_ARGON2_AVX2
	// RANDOMX_FLAG_V2 Implements RandomX V2
	RANDOMX_FLAG_V2

	RANDOMX_FLAG_ARGON2 = RANDOMX_FLAG_ARGON2_AVX2 | RANDOMX_FLAG_ARGON2_SSSE3
)

var ErrLargePagesNotSupported = errors.New("large pages not supported")
