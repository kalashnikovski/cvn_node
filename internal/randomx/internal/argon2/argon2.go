package argon2

import (
	"encoding/binary"
	"golang.org/x/crypto/blake2b"
)

import (
	_ "golang.org/x/crypto/argon2"
	_ "unsafe"
)

const BlockSize uint32 = 1024

type Block [BlockSize / 8]uint64

const syncPoints = 4

//go:linkname initHash golang.org/x/crypto/argon2.initHash
func initHash(password, salt, key, data []byte, time, memory, threads, keyLen uint32, mode int) [blake2b.Size + 8]byte

//go:linkname processBlocks golang.org/x/crypto/argon2.processBlocks
func processBlocks(B []Block, time, memory, threads uint32, mode int)

//go:linkname blake2bHash golang.org/x/crypto/argon2.blake2bHash
func blake2bHash(out []byte, in []byte)

// initBlocks From golang.org/x/crypto/argon2.initBlocks with external memory allocation
func initBlocks(B []Block, h0 *[blake2b.Size + 8]byte, memory, threads uint32) {
	var block0 [1024]byte

	clear(B)

	for lane := uint32(0); lane < threads; lane++ {
		j := lane * (memory / threads)
		binary.LittleEndian.PutUint32(h0[blake2b.Size+4:], lane)

		binary.LittleEndian.PutUint32(h0[blake2b.Size:], 0)
		blake2bHash(block0[:], h0[:])
		for i := range B[j+0] {
			B[j+0][i] = binary.LittleEndian.Uint64(block0[i*8:])
		}

		binary.LittleEndian.PutUint32(h0[blake2b.Size:], 1)
		blake2bHash(block0[:], h0[:])
		for i := range B[j+1] {
			B[j+1][i] = binary.LittleEndian.Uint64(block0[i*8:])
		}
	}
}

// BuildBlocks From golang.org/x/crypto/argon2.deriveKey without last deriveKey call and external memory allocation
func BuildBlocks(B []Block, password, salt []byte, time, memory uint32, threads uint8) {
	if time < 1 {
		panic("argon2: number of rounds too small")
	}
	if threads < 1 {
		panic("argon2: parallelism degree too low")
	}

	if len(B) != int(memory) {
		panic("argon2: invalid block size")
	}

	const mode = 0 /* argon2d */
	const keyLen = 0
	h0 := initHash(password, salt, nil, nil, time, memory, uint32(threads), keyLen, mode)

	memory = memory / (syncPoints * uint32(threads)) * (syncPoints * uint32(threads))
	if memory < 2*syncPoints*uint32(threads) {
		memory = 2 * syncPoints * uint32(threads)
	}

	initBlocks(B, &h0, memory, uint32(threads))
	processBlocks(B, time, memory, uint32(threads), mode)
}
