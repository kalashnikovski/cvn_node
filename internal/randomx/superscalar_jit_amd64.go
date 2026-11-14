//go:build (windows || unix) && amd64 && !disable_jit && !purego

package randomx

import (
	"encoding/binary"

	"cvn_node/internal/randomx/internal/memory"
)

//go:noescape
//go:nosplit
func init_dataset_superscalar_run(itemNumber uint64, constants *[8]uint64, memory *[RANDOMX_ARGON_MEMORY]MemoryBlock, programs uintptr, rl *RegisterLine)

// superscalarMixLoad
// XORQ 0(BX), R8 \
// XORQ 8(BX), R9 \
// XORQ 16(BX), R10 \
// XORQ 24(BX), R11 \
// XORQ 32(BX), R12 \
// XORQ 40(BX), R13 \
// XORQ 48(BX), R14 \
// XORQ 56(BX), R15
var superscalarMixLoad = []byte{
	0x4C, 0x33, 0x03,
	0x4C, 0x33, 0x4B, 0x08,
	0x4C, 0x33, 0x53, 0x10,
	0x4C, 0x33, 0x5B, 0x18,
	0x4C, 0x33, 0x63, 0x20,
	0x4C, 0x33, 0x6B, 0x28,
	0x4C, 0x33, 0x73, 0x30,
	0x4C, 0x33, 0x7B, 0x38,
}

// superscalarMixLoad
// ANDQ $4194303, BX
// SHLQ $6, BX
// ADDQ DI, BX
// PREFETCHNTA 0(BX)
var superscalarLoadPrefetch = []byte{
	0x48, 0x81, 0xE3, 0xFF, 0xFF, 0x3F, 0x00,
	0x48, 0xC1, 0xE3, 0x06,
	0x48, 0x01, 0xFB,
	0x0F, 0x18, 0x03,
}

// generateSuperscalarCode
func generateSuperscalarCode(scalarPrograms []SuperScalarProgram) SuperScalarProgramFunc {

	var program []byte

	program = append(program, superscalarLoadPrefetch...)

	for i, prog := range scalarPrograms {

		p := prog.Program()
		for i := range p {
			instr := &p[i]

			dst := instr.Dst % RegistersCount
			src := instr.Src % RegistersCount

			switch instr.Opcode {
			case S_ISUB_R:
				program = append(program, REX_SUB_RR...)
				program = append(program, byte(0xc0+8*dst+src))
			case S_IXOR_R:
				program = append(program, REX_XOR_RR...)
				program = append(program, byte(0xc0+8*dst+src))
			case S_IADD_RS:
				if instr.Imm32 == 0 {
					program = append(program, REX_ADD_RR...)
					program = append(program, byte(0xc0+8*dst+src))
				} else {
					program = append(program, REX_LEA...)
					program = append(program,
						byte(0x04+8*dst),
						genSIB(int(instr.Imm32), int(src), int(dst)),
					)
				}
			case S_IMUL_R:
				program = append(program, REX_IMUL_RR...)
				program = append(program, byte(0xc0+8*dst+src))
			case S_IROR_C:
				program = append(program, REX_ROT_I8...)
				program = append(program,
					byte(0xc8+dst),
					byte(instr.Imm32&63),
				)
			case S_IADD_C7:
				program = append(program, REX_81...)
				program = append(program, byte(0xc0+dst))
				program = binary.LittleEndian.AppendUint32(program, instr.Imm32)
			case S_IXOR_C7:
				program = append(program, REX_XOR_RI...)
				program = append(program, byte(0xf0+dst))
				program = binary.LittleEndian.AppendUint32(program, instr.Imm32)
			case S_IADD_C8:
				program = append(program, REX_81...)
				program = append(program, byte(0xc0+dst))
				program = binary.LittleEndian.AppendUint32(program, instr.Imm32)
				if CodeAlign > 0 {
					program = append(program, NOP1...)
				}
			case S_IXOR_C8:
				program = append(program, REX_XOR_RI...)
				program = append(program, byte(0xf0+dst))
				program = binary.LittleEndian.AppendUint32(program, instr.Imm32)
				if CodeAlign > 0 {
					program = append(program, NOP1...)
				}

			case S_IADD_C9:
				program = append(program, REX_81...)
				program = append(program, byte(0xc0+dst))
				program = binary.LittleEndian.AppendUint32(program, instr.Imm32)
				if CodeAlign > 0 {
					program = append(program, NOP2...)
				}
			case S_IXOR_C9:
				program = append(program, REX_XOR_RI...)
				program = append(program, byte(0xf0+dst))
				program = binary.LittleEndian.AppendUint32(program, instr.Imm32)
				if CodeAlign > 0 {
					program = append(program, NOP2...)
				}
			case S_IMULH_R:
				program = append(program, REX_MOV_RR64...)
				program = append(program, byte(0xc0+dst))
				program = append(program, REX_MUL_R...)
				program = append(program, byte(0xe0+src))
				program = append(program, REX_MOV_R64R...)
				program = append(program, byte(0xc2+8*dst))
			case S_ISMULH_R:
				program = append(program, REX_MOV_RR64...)
				program = append(program, byte(0xc0+dst))
				program = append(program, REX_MUL_R...)
				program = append(program, byte(0xe8+src))
				program = append(program, REX_MOV_R64R...)
				program = append(program, byte(0xc2+8*dst))
			case S_IMUL_RCP:
				program = append(program, MOV_RAX_I...)
				program = binary.LittleEndian.AppendUint64(program, instr.Imm64)
				program = append(program, REX_IMUL_RM...)
				program = append(program, byte(0xc0+8*instr.Dst))
			default:
				panic("unreachable")
			}
		}

		//TODO: align?

		program = append(program, superscalarMixLoad...)
		if i < (len(scalarPrograms) - 1) {
			program = append(program, REX_MOV_RR64...)
			program = append(program, 0xd8+prog.AddressRegister())
			program = append(program, superscalarLoadPrefetch...)

		}
	}

	program = append(program, RET)

	pagedMemory, err := memory.AllocateSlice[byte](pageAllocator, len(program))
	if err != nil {
		return nil
	}
	copy(pagedMemory, program)

	return pagedMemory
}
