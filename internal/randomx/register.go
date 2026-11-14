package randomx

import (
	"unsafe"

	"cvn_node/internal/randomx/internal/keys"
)

const RegistersCount = 8
const RegistersCountFloat = 4

const LOW = 0
const HIGH = 1

type RegisterLine [RegistersCount]uint64

//go:nosplit
func (rl *RegisterLine) Init(itemNumber uint64) {
	rl[0] = (itemNumber + 1) * keys.SuperScalar_Constants[0]
	rl[1] = rl[0] ^ keys.SuperScalar_Constants[1]
	rl[2] = rl[0] ^ keys.SuperScalar_Constants[2]
	rl[3] = rl[0] ^ keys.SuperScalar_Constants[3]
	rl[4] = rl[0] ^ keys.SuperScalar_Constants[4]
	rl[5] = rl[0] ^ keys.SuperScalar_Constants[5]
	rl[6] = rl[0] ^ keys.SuperScalar_Constants[6]
	rl[7] = rl[0] ^ keys.SuperScalar_Constants[7]
}

type RegisterFile struct {
	R RegisterLine
	F [RegistersCountFloat][2]float64
	E [RegistersCountFloat][2]float64
	A [RegistersCountFloat][2]float64

	FPRC uint8
}

const RegisterFileSize = RegistersCount*8 + RegistersCountFloat*2*8*3

//go:nosplit
func (rf *RegisterFile) MemoryR() *[RegistersCount * 8]byte {
	return (*[RegistersCount * 8]byte)(unsafe.Pointer(&rf.R))
}

//go:nosplit
func (rf *RegisterFile) MemoryF() *[RegistersCountFloat * 2 * 8]byte {
	return (*[RegistersCountFloat * 2 * 8]byte)(unsafe.Pointer(&rf.F))
}

//go:nosplit
func (rf *RegisterFile) MemoryE() *[RegistersCountFloat * 2 * 8]byte {
	return (*[RegistersCountFloat * 2 * 8]byte)(unsafe.Pointer(&rf.E))
}

//go:nosplit
func (rf *RegisterFile) Memory() *[RegisterFileSize]byte {
	return (*[RegisterFileSize]byte)(unsafe.Pointer(rf))
}

//go:nosplit
func (rf *RegisterFile) Clear() {
	clear(rf.Memory()[:])
}
