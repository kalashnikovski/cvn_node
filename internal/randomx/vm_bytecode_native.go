//go:build (arm64 || arm.6 || arm.7 || amd64 || 386) && !purego

package randomx

import (
	"math"
	"math/bits"

	"cvn_node/internal/randomx/internal/asm"
)

// Execute Runs a RandomX program with the given register file and scratchpad
// Warning: This will call asm.SetRoundingMode directly
// It is the caller's responsibility to set and restore the mode to RoundingModeToNearest between full executions
// Additionally, runtime.LockOSThread and defer runtime.UnlockOSThread is recommended to prevent other goroutines sharing these changes
func (c *ByteCode) Execute(f *RegisterFile, pad *ScratchPad, eMask [2]uint64, v2 bool) {
	for pc := 0; pc < len(c); pc++ {
		i := &c[pc]
		switch i.Opcode {
		case VM_NOP: // we do nothing
		case VM_IADD_RS:
			f.R[i.Dst] += (f.R[i.Src] << i.ImmB) + i.Imm
		case VM_IADD_M:
			f.R[i.Dst] += pad.Load64(i.getScratchpadAddress(f.R[i.Src]))
		case VM_IADD_MZ:
			f.R[i.Dst] += pad.Load64(uint32(i.Imm))
		case VM_ISUB_R:
			f.R[i.Dst] -= f.R[i.Src]
		case VM_ISUB_I:
			f.R[i.Dst] -= i.Imm
		case VM_ISUB_M:
			f.R[i.Dst] -= pad.Load64(i.getScratchpadAddress(f.R[i.Src]))
		case VM_ISUB_MZ:
			f.R[i.Dst] -= pad.Load64(uint32(i.Imm))
		case VM_IMUL_R:
			f.R[i.Dst] *= f.R[i.Src]
		case VM_IMUL_I:
			// also handles imul_rcp
			f.R[i.Dst] *= i.Imm
		case VM_IMUL_M:
			f.R[i.Dst] *= pad.Load64(i.getScratchpadAddress(f.R[i.Src]))
		case VM_IMUL_MZ:
			f.R[i.Dst] *= pad.Load64(uint32(i.Imm))
		case VM_IMULH_R:
			f.R[i.Dst], _ = bits.Mul64(f.R[i.Dst], f.R[i.Src])
		case VM_IMULH_M:
			f.R[i.Dst], _ = bits.Mul64(f.R[i.Dst], pad.Load64(i.getScratchpadAddress(f.R[i.Src])))
		case VM_IMULH_MZ:
			f.R[i.Dst], _ = bits.Mul64(f.R[i.Dst], pad.Load64(uint32(i.Imm)))
		case VM_ISMULH_R:
			f.R[i.Dst] = smulh(int64(f.R[i.Dst]), int64(f.R[i.Src]))
		case VM_ISMULH_M:
			f.R[i.Dst] = smulh(int64(f.R[i.Dst]), int64(pad.Load64(i.getScratchpadAddress(f.R[i.Src]))))
		case VM_ISMULH_MZ:
			f.R[i.Dst] = smulh(int64(f.R[i.Dst]), int64(pad.Load64(uint32(i.Imm))))
		case VM_INEG_R:
			f.R[i.Dst] = -f.R[i.Dst]
		case VM_IXOR_R:
			f.R[i.Dst] ^= f.R[i.Src]
		case VM_IXOR_I:
			f.R[i.Dst] ^= i.Imm
		case VM_IXOR_M:
			f.R[i.Dst] ^= pad.Load64(i.getScratchpadAddress(f.R[i.Src]))
		case VM_IXOR_MZ:
			f.R[i.Dst] ^= pad.Load64(uint32(i.Imm))
		case VM_IROR_R:
			f.R[i.Dst] = bits.RotateLeft64(f.R[i.Dst], 0-int(f.R[i.Src]&63))
		case VM_IROR_I:
			//todo: can merge into VM_IROL_I
			f.R[i.Dst] = bits.RotateLeft64(f.R[i.Dst], 0-int(i.Imm&63))
		case VM_IROL_R:
			f.R[i.Dst] = bits.RotateLeft64(f.R[i.Dst], int(f.R[i.Src]&63))
		case VM_IROL_I:
			f.R[i.Dst] = bits.RotateLeft64(f.R[i.Dst], int(i.Imm&63))
		case VM_ISWAP_R:
			f.R[i.Dst], f.R[i.Src] = f.R[i.Src], f.R[i.Dst]

		case VM_FSWAP_RF:
			f.F[i.Dst][HIGH], f.F[i.Dst][LOW] = f.F[i.Dst][LOW], f.F[i.Dst][HIGH]
		case VM_FSWAP_RE:
			f.E[i.Dst][HIGH], f.E[i.Dst][LOW] = f.E[i.Dst][LOW], f.E[i.Dst][HIGH]
		case VM_FADD_R:
			f.F[i.Dst][LOW] += f.A[i.Src][LOW]
			f.F[i.Dst][HIGH] += f.A[i.Src][HIGH]
		case VM_FADD_M:
			lo, hi := pad.Load32F(i.getScratchpadAddress(f.R[i.Src]))
			f.F[i.Dst][LOW] += lo
			f.F[i.Dst][HIGH] += hi
		case VM_FSUB_R:
			f.F[i.Dst][LOW] -= f.A[i.Src][LOW]
			f.F[i.Dst][HIGH] -= f.A[i.Src][HIGH]
		case VM_FSUB_M:
			lo, hi := pad.Load32F(i.getScratchpadAddress(f.R[i.Src]))
			f.F[i.Dst][LOW] -= lo
			f.F[i.Dst][HIGH] -= hi
		case VM_FSCAL_R:
			// no dependent on rounding modes
			f.F[i.Dst][LOW] = ScaleNegate(f.F[i.Dst][LOW])
			f.F[i.Dst][HIGH] = ScaleNegate(f.F[i.Dst][HIGH])
		case VM_FMUL_R:
			f.E[i.Dst][LOW] *= f.A[i.Src][LOW]
			f.E[i.Dst][HIGH] *= f.A[i.Src][HIGH]
		case VM_FDIV_M:
			lo, hi := pad.Load32F(i.getScratchpadAddress(f.R[i.Src]))
			f.E[i.Dst][LOW] /= MaskRegisterExponentMantissa(lo, eMask[LOW])
			f.E[i.Dst][HIGH] /= MaskRegisterExponentMantissa(hi, eMask[HIGH])
		case VM_FSQRT_R:
			f.E[i.Dst][LOW] = math.Sqrt(f.E[i.Dst][LOW])
			f.E[i.Dst][HIGH] = math.Sqrt(f.E[i.Dst][HIGH])
		case VM_CFROUND:
			isrc := (bits.RotateLeft64(f.R[i.Src], 0-int(i.Imm))) // rotate right
			if !v2 || (isrc&60) == 0 {
				SetRoundingMode(f, uint8(isrc%4))
			}
		case VM_CBRANCH:
			f.R[i.Dst] += i.Imm
			if (f.R[i.Dst] & uint64(i.MemMask)) == 0 {
				pc = i.jumpTarget()
			}
		case VM_ISTORE:
			pad.Store64(i.getScratchpadAddress(f.R[i.Dst]), f.R[i.Src])
		}
	}
}

const lockThreadDueToRoundingMode = true

func SetRoundingMode(f *RegisterFile, mode uint8) {
	if f.FPRC == mode {
		return
	}
	f.FPRC = mode
	asm.SetRoundingMode(mode)
}

func ResetRoundingMode(f *RegisterFile) {
	f.FPRC = 0
	asm.SetRoundingMode(uint8(0))
}
