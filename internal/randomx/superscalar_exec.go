package randomx

import (
	"fmt"
	"math/bits"
)

type SuperScalarCode uint8

func (code SuperScalarCode) String() string {
	switch code {
	case SS_ISUB_R:
		return "ISUB_R"
	case SS_IXOR_R:
		return "IXOR_R"
	case SS_IADD_R:
		return "IADD_R"
	case SS_IADD_RS:
		return "IADD_RS"
	case SS_IMUL_R:
		return "IMUL_R"
	case SS_IMULH_R:
		return "IMULH_R"
	case SS_ISMULH_R:
		return "ISMULH_R"
	case SS_IROL_C:
		return "IROL_C"
	case SS_IADD_C:
		return "IADD_C"
	case SS_IXOR_C:
		return "IXOR_C"
	case SS_IMUL_C:
		return "IMUL_C"
	default:
		panic("unknown code")
	}
}

const (
	SS_ISUB_R = SuperScalarCode(iota)
	SS_IXOR_R
	SS_IADD_R
	SS_IADD_RS
	SS_IMUL_R
	SS_IMULH_R
	SS_ISMULH_R
	SS_IROL_C
	SS_IADD_C
	SS_IXOR_C
	SS_IMUL_C
)

type SuperScalarOp struct {
	Opcode   SuperScalarCode
	Dst, Src uint8
	Imm      uint64
}

func (op SuperScalarOp) String() string {
	switch op.Opcode {
	case SS_ISUB_R:
		return fmt.Sprintf("r%x -= r%x", op.Dst, op.Src)
	case SS_IXOR_R:
		return fmt.Sprintf("r%x ^= r%x", op.Dst, op.Src)
	case SS_IADD_R:
		return fmt.Sprintf("r%x += r%x", op.Dst, op.Src)
	case SS_IADD_RS:
		return fmt.Sprintf("r%x += r%x << %d", op.Dst, op.Src, op.Imm)
	case SS_IMUL_R:
		return fmt.Sprintf("r%x *= r%x", op.Dst, op.Src)
	case SS_IMULH_R:
		return fmt.Sprintf("r%x, _ = (uint) r%x * r%x", op.Dst, op.Dst, op.Src)
	case SS_ISMULH_R:
		return fmt.Sprintf("r%x, _ = (int) r%x * r%x", op.Dst, op.Dst, op.Src)
	case SS_IROL_C:
		return fmt.Sprintf("r%x <<= %d", op.Dst, op.Imm)
	case SS_IADD_C:
		return fmt.Sprintf("r%x += 0x%016d", op.Dst, op.Imm)
	case SS_IXOR_C:
		return fmt.Sprintf("r%x ^= 0x%016d", op.Dst, op.Imm)
	case SS_IMUL_C:
		return fmt.Sprintf("r%x *= 0x%016d", op.Dst, op.Imm)
	default:
		panic("unknown opcode")
	}
}

func buildSuperscalar(p SuperScalarProgram) []SuperScalarOp {
	prog := p.Program()
	ops := make([]SuperScalarOp, 0, len(prog))

	for _, ins := range prog {
		switch ins.Opcode {
		case S_ISUB_R:
			ops = append(ops, SuperScalarOp{
				Opcode: SS_ISUB_R,
				Dst:    ins.Dst,
				Src:    ins.Src,
			})
		case S_IXOR_R:
			ops = append(ops, SuperScalarOp{
				Opcode: SS_IXOR_R,
				Dst:    ins.Dst,
				Src:    ins.Src,
			})
		case S_IADD_RS:
			if ins.Imm32 == 0 {
				//convert to S_IADD_R
				ops = append(ops, SuperScalarOp{
					Opcode: SS_IADD_R,
					Dst:    ins.Dst,
					Src:    ins.Src,
				})
				continue
			}
			ops = append(ops, SuperScalarOp{
				Opcode: SS_IADD_RS,
				Dst:    ins.Dst,
				Src:    ins.Src,
				Imm:    uint64(ins.Imm32),
			})
		case S_IMUL_R:
			ops = append(ops, SuperScalarOp{
				Opcode: SS_IMUL_R,
				Dst:    ins.Dst,
				Src:    ins.Src,
			})
		case S_IROR_C:
			if ins.Imm32 == 0 {
				continue
			}
			ops = append(ops, SuperScalarOp{
				Opcode: SS_IROL_C,
				Dst:    ins.Dst,
				// preshift rotate right to rotate left
				Imm: 64 - uint64(ins.Imm32),
			})
		case S_IADD_C7, S_IADD_C8, S_IADD_C9:
			imm := signExtend2sCompl(ins.Imm32)
			if imm == 0 {
				continue
			}
			ops = append(ops, SuperScalarOp{
				Opcode: SS_IADD_C,
				Dst:    ins.Dst,
				Imm:    imm,
			})
		case S_IXOR_C7, S_IXOR_C8, S_IXOR_C9:
			imm := signExtend2sCompl(ins.Imm32)
			if imm == 0 {
				continue
			}
			ops = append(ops, SuperScalarOp{
				Opcode: SS_IXOR_C,
				Dst:    ins.Dst,
				Imm:    imm,
			})
		case S_IMULH_R:
			ops = append(ops, SuperScalarOp{
				Opcode: SS_IMULH_R,
				Dst:    ins.Dst,
				Src:    ins.Src,
			})
		case S_ISMULH_R:
			ops = append(ops, SuperScalarOp{
				Opcode: SS_ISMULH_R,
				Dst:    ins.Dst,
				Src:    ins.Src,
			})
		case S_IMUL_RCP:
			if ins.Imm64 == 1 {
				// skip same output
				continue
			}
			ops = append(ops, SuperScalarOp{
				Opcode: SS_IMUL_C,
				Dst:    ins.Dst,
				Imm:    ins.Imm64,
			})
		}
	}

	return ops
}

// executeSuperscalar execute the superscalar program
//
//go:nosplit
func executeSuperscalar(p []SuperScalarOp, rl RegisterLine) RegisterLine {
	for _, ins := range p {
		switch ins.Opcode & 0b111 {
		case SS_ISUB_R:
			rl[ins.Dst&0b111] -= rl[ins.Src&0b111]
		case SS_IXOR_R:
			rl[ins.Dst&0b111] ^= rl[ins.Src&0b111]
		case SS_IADD_R:
			rl[ins.Dst&0b111] += rl[ins.Src&0b111]
		case SS_IADD_RS:
			rl[ins.Dst&0b111] += rl[ins.Src&0b111] << ins.Imm
		case SS_IMUL_R:
			rl[ins.Dst&0b111] *= rl[ins.Src&0b111]
		case SS_IMULH_R:
			rl[ins.Dst&0b111], _ = bits.Mul64(rl[ins.Dst&0b111], rl[ins.Src&0b111])
		case SS_ISMULH_R:
			rl[ins.Dst&0b111] = smulh(int64(rl[ins.Dst&0b111]), int64(rl[ins.Src&0b111]))
		case SS_IROL_C:
			rl[ins.Dst&0b111] = bits.RotateLeft64(rl[ins.Dst&0b111], int(ins.Imm))
		case SS_IADD_C:
			rl[ins.Dst&0b111] += ins.Imm
		case SS_IXOR_C:
			rl[ins.Dst&0b111] ^= ins.Imm
		case SS_IMUL_C:
			rl[ins.Dst&0b111] *= ins.Imm
		}
	}

	return rl
}
