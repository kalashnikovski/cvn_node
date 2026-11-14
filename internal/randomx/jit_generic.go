//go:build !(windows || unix) || !amd64 || disable_jit || purego

package randomx

const supportsJIT = false

var RandomXCodeSize uint64 = 0
