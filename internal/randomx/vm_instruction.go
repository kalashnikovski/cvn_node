/*
Copyright (c) 2019 DERO Foundation. All rights reserved.

Redistribution and use in source and binary forms, with or without modification,
are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice,
this list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
this list of conditions and the following disclaimer in the documentation
and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its contributors
may be used to endorse or promote products derived from this software without
specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE
LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE
USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/

package randomx

import (
	"unsafe"
)
import "encoding/binary"

//reference https://github.com/tevador/RandomX/blob/master/doc/specs.md#51-instruction-encoding

// VM_Instruction since go does not have union, use byte array
type VM_Instruction [8]byte // it is hardcode 8 bytes

func (ins VM_Instruction) IMM() uint32 {
	return binary.LittleEndian.Uint32(ins[4:])
}

func (ins VM_Instruction) IMM64() uint64 {
	return signExtend2sCompl(ins.IMM())
}

func (ins VM_Instruction) Mod() byte {
	return ins[3]
}
func (ins VM_Instruction) Src() byte {
	return ins[2]
}
func (ins VM_Instruction) Dst() byte {
	return ins[1]
}
func (ins VM_Instruction) Opcode() byte {
	return ins[0]
}

// CompileProgramToByteCode this will interpret single vm instruction into executable opcodes
// reference https://github.com/tevador/RandomX/blob/master/doc/specs.md#52-integer-instructions
func CompileProgramToByteCode(prog []byte, bc *ByteCode) {

	var registerUsage [RegistersCount]int
	for i := range registerUsage {
		registerUsage[i] = -1
	}

	for i := 0; i < len(bc); i++ {
		instr := VM_Instruction(prog[i*8:])
		ibc := &bc[i]

		opcode := instr.Opcode()
		dst := instr.Dst() % RegistersCount // bit shift optimization
		src := instr.Src() % RegistersCount
		ibc.Dst = dst
		ibc.Src = src
		switch opcode {
		case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15: // 16 frequency
			ibc.Opcode = VM_IADD_RS
			if dst != RegisterNeedsDisplacement {
				//shift
				ibc.ImmB = (instr.Mod() >> 2) % 4
				ibc.Imm = 0
			} else {
				//shift
				ibc.ImmB = (instr.Mod() >> 2) % 4
				ibc.Imm = instr.IMM64()
			}
			registerUsage[dst] = i

		case 16, 17, 18, 19, 20, 21, 22: // 7
			ibc.Opcode = VM_IADD_M
			ibc.Imm = instr.IMM64()
			if src != dst {
				if (instr.Mod() % 4) != 0 {
					ibc.MemMask = ScratchpadL1Mask
				} else {
					ibc.MemMask = ScratchpadL2Mask
				}
			} else {
				ibc.Opcode = VM_IADD_MZ
				ibc.MemMask = ScratchpadL3Mask
				ibc.Imm = uint64(ibc.getScratchpadZeroAddress())
			}
			registerUsage[dst] = i
		case 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38: // 16
			ibc.Opcode = VM_ISUB_R

			if src == dst {
				ibc.Imm = instr.IMM64()
				ibc.Opcode = VM_ISUB_I
			}
			registerUsage[dst] = i
		case 39, 40, 41, 42, 43, 44, 45: // 7
			ibc.Opcode = VM_ISUB_M
			ibc.Imm = instr.IMM64()
			if src != dst {
				if (instr.Mod() % 4) != 0 {
					ibc.MemMask = ScratchpadL1Mask
				} else {
					ibc.MemMask = ScratchpadL2Mask
				}
			} else {
				ibc.Opcode = VM_ISUB_MZ
				ibc.MemMask = ScratchpadL3Mask
				ibc.Imm = uint64(ibc.getScratchpadZeroAddress())
			}
			registerUsage[dst] = i
		case 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61: // 16
			ibc.Opcode = VM_IMUL_R

			if src == dst {
				ibc.Imm = instr.IMM64()
				ibc.Opcode = VM_IMUL_I
			}
			registerUsage[dst] = i
		case 62, 63, 64, 65: //4
			ibc.Opcode = VM_IMUL_M
			ibc.Imm = instr.IMM64()
			if src != dst {
				if (instr.Mod() % 4) != 0 {
					ibc.MemMask = ScratchpadL1Mask
				} else {
					ibc.MemMask = ScratchpadL2Mask
				}
			} else {
				ibc.Opcode = VM_IMUL_MZ
				ibc.MemMask = ScratchpadL3Mask
				ibc.Imm = uint64(ibc.getScratchpadZeroAddress())
			}
			registerUsage[dst] = i
		case 66, 67, 68, 69: //4
			ibc.Opcode = VM_IMULH_R
			registerUsage[dst] = i
		case 70: //1
			ibc.Opcode = VM_IMULH_M
			ibc.Imm = instr.IMM64()
			if src != dst {
				if (instr.Mod() % 4) != 0 {
					ibc.MemMask = ScratchpadL1Mask
				} else {
					ibc.MemMask = ScratchpadL2Mask
				}
			} else {
				ibc.Opcode = VM_IMULH_MZ
				ibc.MemMask = ScratchpadL3Mask
				ibc.Imm = uint64(ibc.getScratchpadZeroAddress())
			}
			registerUsage[dst] = i
		case 71, 72, 73, 74: //4
			ibc.Opcode = VM_ISMULH_R
			registerUsage[dst] = i
		case 75: //1
			ibc.Opcode = VM_ISMULH_M
			ibc.Imm = instr.IMM64()
			if src != dst {
				if (instr.Mod() % 4) != 0 {
					ibc.MemMask = ScratchpadL1Mask
				} else {
					ibc.MemMask = ScratchpadL2Mask
				}
			} else {
				ibc.Opcode = VM_ISMULH_MZ
				ibc.MemMask = ScratchpadL3Mask
				ibc.Imm = uint64(ibc.getScratchpadZeroAddress())
			}
			registerUsage[dst] = i
		case 76, 77, 78, 79, 80, 81, 82, 83: // 8
			divisor := instr.IMM()
			if !isZeroOrPowerOf2(divisor) {
				ibc.Opcode = VM_IMUL_I
				ibc.Imm = reciprocal(divisor)
				registerUsage[dst] = i
			} else {
				ibc.Opcode = VM_NOP
			}

		case 84, 85: //2
			ibc.Opcode = VM_INEG_R
			registerUsage[dst] = i
		case 86, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 97, 98, 99, 100: //15
			ibc.Opcode = VM_IXOR_R

			if src == dst {
				ibc.Imm = instr.IMM64()
				ibc.Opcode = VM_IXOR_I
			}
			registerUsage[dst] = i
		case 101, 102, 103, 104, 105: //5
			ibc.Opcode = VM_IXOR_M
			ibc.Imm = instr.IMM64()
			if src != dst {
				if (instr.Mod() % 4) != 0 {
					ibc.MemMask = ScratchpadL1Mask
				} else {
					ibc.MemMask = ScratchpadL2Mask
				}
			} else {
				ibc.Opcode = VM_IXOR_MZ
				ibc.MemMask = ScratchpadL3Mask
				ibc.Imm = uint64(ibc.getScratchpadZeroAddress())
			}
			registerUsage[dst] = i
		case 106, 107, 108, 109, 110, 111, 112, 113: //8
			ibc.Opcode = VM_IROR_R
			if src == dst {
				ibc.Imm = instr.IMM64()
				ibc.Opcode = VM_IROR_I
			}
			registerUsage[dst] = i
		case 114, 115: // 2 IROL_R
			ibc.Opcode = VM_IROL_R

			if src == dst {
				ibc.Imm = instr.IMM64()
				ibc.Opcode = VM_IROL_I
			}
			registerUsage[dst] = i

		case 116, 117, 118, 119: //4
			if src != dst {
				ibc.Opcode = VM_ISWAP_R
				registerUsage[dst] = i
				registerUsage[src] = i
			} else {
				ibc.Opcode = VM_NOP

			}

		// below are floating point instructions
		case 120, 121, 122, 123: // 4
			//ibc.Opcode = VM_FSWAP_R
			if dst < RegistersCountFloat {
				ibc.Opcode = VM_FSWAP_RF
			} else {
				ibc.Opcode = VM_FSWAP_RE
				ibc.Dst = dst - RegistersCountFloat
			}
		case 124, 125, 126, 127, 128, 129, 130, 131, 132, 133, 134, 135, 136, 137, 138, 139: //16
			ibc.Dst = instr.Dst() % RegistersCountFloat // bit shift optimization
			ibc.Src = instr.Src() % RegistersCountFloat
			ibc.Opcode = VM_FADD_R

		case 140, 141, 142, 143, 144: //5
			ibc.Dst = instr.Dst() % RegistersCountFloat // bit shift optimization
			ibc.Opcode = VM_FADD_M
			if (instr.Mod() % 4) != 0 {
				ibc.MemMask = ScratchpadL1Mask
			} else {
				ibc.MemMask = ScratchpadL2Mask
			}
			ibc.Imm = instr.IMM64()

		case 145, 146, 147, 148, 149, 150, 151, 152, 153, 154, 155, 156, 157, 158, 159, 160: //16
			ibc.Dst = instr.Dst() % RegistersCountFloat // bit shift optimization
			ibc.Src = instr.Src() % RegistersCountFloat
			ibc.Opcode = VM_FSUB_R
		case 161, 162, 163, 164, 165: //5
			ibc.Dst = instr.Dst() % RegistersCountFloat // bit shift optimization
			ibc.Opcode = VM_FSUB_M
			if (instr.Mod() % 4) != 0 {
				ibc.MemMask = ScratchpadL1Mask
			} else {
				ibc.MemMask = ScratchpadL2Mask
			}
			ibc.Imm = instr.IMM64()

		case 166, 167, 168, 169, 170, 171: //6
			ibc.Dst = instr.Dst() % RegistersCountFloat // bit shift optimization
			ibc.Opcode = VM_FSCAL_R
		case 172, 173, 174, 175, 176, 177, 178, 179, 180, 181, 182, 183, 184, 185, 186, 187, 188, 189, 190, 191, 192, 193, 194, 195, 196, 197, 198, 199, 200, 201, 202, 203: //32
			ibc.Dst = instr.Dst() % RegistersCountFloat // bit shift optimization
			ibc.Src = instr.Src() % RegistersCountFloat
			ibc.Opcode = VM_FMUL_R
		case 204, 205, 206, 207: //4
			ibc.Dst = instr.Dst() % RegistersCountFloat // bit shift optimization
			ibc.Opcode = VM_FDIV_M
			if (instr.Mod() % 4) != 0 {
				ibc.MemMask = ScratchpadL1Mask
			} else {
				ibc.MemMask = ScratchpadL2Mask
			}
			ibc.Imm = instr.IMM64()
		case 208, 209, 210, 211, 212, 213: //6
			ibc.Dst = instr.Dst() % RegistersCountFloat // bit shift optimization
			ibc.Opcode = VM_FSQRT_R

		case 214, 215, 216, 217, 218, 219, 220, 221, 222, 223, 224, 225, 226, 227, 228, 229, 230, 231, 232, 233, 234, 235, 236, 237, 238: //25  // CBRANCH and CFROUND are interchanged
			ibc.Opcode = VM_CBRANCH
			//TODO:??? it's +1 on other
			ibc.Dst = instr.Dst() % RegistersCount

			target := uint16(int16(registerUsage[ibc.Dst]))
			// set target!
			ibc.Src = uint8(target)
			ibc.ImmB = uint8(target >> 8)

			shift := uint64(instr.Mod()>>4) + CONDITIONOFFSET
			//conditionmask := CONDITIONMASK << shift
			ibc.Imm = instr.IMM64() | (uint64(1) << shift)
			if CONDITIONOFFSET > 0 || shift > 0 {
				ibc.Imm &= ^(uint64(1) << (shift - 1))
			}
			ibc.MemMask = CONDITIONMASK << shift

			for j := 0; j < RegistersCount; j++ {
				registerUsage[j] = i
			}

		case 239: //1
			ibc.Opcode = VM_CFROUND
			ibc.Imm = uint64(instr.IMM() & 63)

		case 240, 241, 242, 243, 244, 245, 246, 247, 248, 249, 250, 251, 252, 253, 254, 255: //16
			ibc.Opcode = VM_ISTORE
			ibc.Imm = instr.IMM64()
			if (instr.Mod() >> 4) < STOREL3CONDITION {
				if (instr.Mod() % 4) != 0 {
					ibc.MemMask = ScratchpadL1Mask
				} else {
					ibc.MemMask = ScratchpadL2Mask
				}

			} else {
				ibc.MemMask = ScratchpadL3Mask
			}

		default:
			panic("unreachable")

		}
	}
}

type ScratchPad [ScratchpadSize]byte

//go:nosplit
func (pad *ScratchPad) Store64(addr uint32, val uint64) {
	*(*uint64)(unsafe.Pointer(&pad[addr])) = val
	//binary.LittleEndian.PutUint64(pad[addr:], val)
}

//go:nosplit
func (pad *ScratchPad) Load64x8(addr uint32) *[8 * 8]byte {
	return (*[8 * 8]byte)(unsafe.Pointer(&pad[addr]))
}

//go:nosplit
func (pad *ScratchPad) Load64(addr uint32) uint64 {
	return *(*uint64)(unsafe.Pointer(&pad[addr]))
}

//go:nosplit
func (pad *ScratchPad) Load32(addr uint32) uint32 {
	return *(*uint32)(unsafe.Pointer(&pad[addr]))
}
