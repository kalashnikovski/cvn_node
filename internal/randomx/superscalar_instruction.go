package randomx

import "cvn_node/internal/randomx/internal/blake2"

// SuperScalarInstruction superscalar program is built with superscalar instructions
type SuperScalarInstruction struct {
	Opcode           byte
	Dst              uint8
	Src              uint8
	Mod              byte
	Imm32            uint32
	Imm64            uint64
	OpGroup          int
	OpGroupPar       int
	GroupParIsSource int
	ins              *Instruction
	CanReuse         bool
}

func (sins *SuperScalarInstruction) FixSrcReg() {
	if sins.Src == 0xff {
		sins.Src = sins.Dst
	}

}
func (sins *SuperScalarInstruction) Reset() {
	sins.Opcode = 99
	sins.Src = 0xff
	sins.Dst = 0xff
	sins.CanReuse = false
	sins.GroupParIsSource = 0
}

func createSuperScalarInstruction(sins *SuperScalarInstruction, ins *Instruction, gen *blake2.Generator) {
	sins.Reset()
	sins.ins = ins
	sins.OpGroupPar = -1
	sins.Opcode = ins.Opcode

	switch ins.Opcode {
	case S_ISUB_R:
		sins.Mod = 0
		sins.Imm32 = 0
		sins.OpGroup = S_IADD_RS
		sins.GroupParIsSource = 1
	case S_IXOR_R:
		sins.Mod = 0
		sins.Imm32 = 0
		sins.OpGroup = S_IXOR_R
		sins.GroupParIsSource = 1
	case S_IADD_RS:
		sins.Mod = gen.GetByte()
		// set modshift on Imm32
		sins.Imm32 = uint32((sins.Mod >> 2) % 4) // bits 2-3
		//sins.Imm32 = 0
		sins.OpGroup = S_IADD_RS
		sins.GroupParIsSource = 1
	case S_IMUL_R:
		sins.Mod = 0
		sins.Imm32 = 0
		sins.OpGroup = S_IMUL_R
		sins.GroupParIsSource = 1
	case S_IROR_C:
		sins.Mod = 0

		for sins.Imm32 = 0; sins.Imm32 == 0; {
			sins.Imm32 = uint32(gen.GetByte() & 63)
		}

		sins.OpGroup = S_IROR_C
		sins.OpGroupPar = -1
	case S_IADD_C7, S_IADD_C8, S_IADD_C9:
		sins.Mod = 0
		sins.Imm32 = gen.GetUint32()
		sins.OpGroup = S_IADD_C7
		sins.OpGroupPar = -1
	case S_IXOR_C7, S_IXOR_C8, S_IXOR_C9:
		sins.Mod = 0
		sins.Imm32 = gen.GetUint32()
		sins.OpGroup = S_IXOR_C7
		sins.OpGroupPar = -1

	case S_IMULH_R:
		sins.CanReuse = true
		sins.Mod = 0
		sins.Imm32 = 0
		sins.OpGroup = S_IMULH_R
		sins.OpGroupPar = int(gen.GetUint32())
	case S_ISMULH_R:
		sins.CanReuse = true
		sins.Mod = 0
		sins.Imm32 = 0
		sins.OpGroup = S_ISMULH_R
		sins.OpGroupPar = int(gen.GetUint32())

	case S_IMUL_RCP:

		sins.Mod = 0
		for {
			sins.Imm32 = gen.GetUint32()
			if (sins.Imm32&sins.Imm32 - 1) != 0 {
				break
			}
		}

		sins.Imm64 = reciprocal(sins.Imm32)

		sins.OpGroup = S_IMUL_RCP

	default:
		panic("should not occur")

	}

}

var slot3 = []*Instruction{&ISUB_R, &IXOR_R} // 3 length instruction will be filled with these
var slot3L = []*Instruction{&ISUB_R, &IXOR_R, &IMULH_R, &ISMULH_R}

var slot4 = []*Instruction{&IROR_C, &IADD_RS}
var slot7 = []*Instruction{&IXOR_C7, &IADD_C7}
var slot8 = []*Instruction{&IXOR_C8, &IADD_C8}
var slot9 = []*Instruction{&IXOR_C9, &IADD_C9}
var slot10 = []*Instruction{&IMUL_RCP}

func CreateSuperScalarInstruction(sins *SuperScalarInstruction, gen *blake2.Generator, instructionLen int, decoderType DecoderType, last, first bool) {

	switch instructionLen {
	case 3:
		if last {
			createSuperScalarInstruction(sins, slot3L[gen.GetByte()&3], gen)
		} else {
			createSuperScalarInstruction(sins, slot3[gen.GetByte()&1], gen)
		}
	case 4:
		//if this is the 4-4-4-4 buffer, issue multiplications as the first 3 instructions
		if decoderType == Decoder4444 && !last {
			createSuperScalarInstruction(sins, &IMUL_R, gen)
		} else {
			createSuperScalarInstruction(sins, slot4[gen.GetByte()&1], gen)
		}
	case 7:
		createSuperScalarInstruction(sins, slot7[gen.GetByte()&1], gen)

	case 8:
		createSuperScalarInstruction(sins, slot8[gen.GetByte()&1], gen)

	case 9:
		createSuperScalarInstruction(sins, slot9[gen.GetByte()&1], gen)
	case 10:
		createSuperScalarInstruction(sins, slot10[0], gen)

	default:
		panic("should not be possible")
	}

}
