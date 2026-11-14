//go:build (!arm64 && !(arm.6 || arm.7) && !amd64 && !386) || purego

package randomx

import (
	"math"
	"math/bits"
	"unsafe"
)

// Execute Runs a RandomX program with the given register file and scratchpad
// Warning: This will call float64 SetRoundingMode directly
// It is the caller's responsibility to set and restore the mode to IEEE 754 roundTiesToEven between full executions
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
			switch f.FPRC & 3 {
			case 0:
				f.F[i.Dst][LOW] = f.F[i.Dst][LOW] + f.A[i.Src][LOW]
				f.F[i.Dst][HIGH] = f.F[i.Dst][HIGH] + f.A[i.Src][HIGH]
			case 1:
				f.F[i.Dst][LOW] = fadd_1(f.F[i.Dst][LOW], f.A[i.Src][LOW])
				f.F[i.Dst][HIGH] = fadd_1(f.F[i.Dst][HIGH], f.A[i.Src][HIGH])
			case 2:
				f.F[i.Dst][LOW] = fadd_2(f.F[i.Dst][LOW], f.A[i.Src][LOW])
				f.F[i.Dst][HIGH] = fadd_2(f.F[i.Dst][HIGH], f.A[i.Src][HIGH])
			case 3:
				f.F[i.Dst][LOW] = fadd_3(f.F[i.Dst][LOW], f.A[i.Src][LOW])
				f.F[i.Dst][HIGH] = fadd_3(f.F[i.Dst][HIGH], f.A[i.Src][HIGH])
			}
		case VM_FADD_M:
			lo, hi := pad.Load32F(i.getScratchpadAddress(f.R[i.Src]))

			switch f.FPRC & 3 {
			case 0:
				f.F[i.Dst][LOW] = f.F[i.Dst][LOW] + lo
				f.F[i.Dst][HIGH] = f.F[i.Dst][HIGH] + hi
			case 1:
				f.F[i.Dst][LOW] = fadd_1(f.F[i.Dst][LOW], lo)
				f.F[i.Dst][HIGH] = fadd_1(f.F[i.Dst][HIGH], hi)
			case 2:
				f.F[i.Dst][LOW] = fadd_2(f.F[i.Dst][LOW], lo)
				f.F[i.Dst][HIGH] = fadd_2(f.F[i.Dst][HIGH], hi)
			case 3:
				f.F[i.Dst][LOW] = fadd_3(f.F[i.Dst][LOW], lo)
				f.F[i.Dst][HIGH] = fadd_3(f.F[i.Dst][HIGH], hi)
			}
		case VM_FSUB_R:
			switch f.FPRC & 3 {
			case 0:
				f.F[i.Dst][LOW] = f.F[i.Dst][LOW] - f.A[i.Src][LOW]
				f.F[i.Dst][HIGH] = f.F[i.Dst][HIGH] - f.A[i.Src][HIGH]
			case 1:
				f.F[i.Dst][LOW] = fsub_1(f.F[i.Dst][LOW], f.A[i.Src][LOW])
				f.F[i.Dst][HIGH] = fsub_1(f.F[i.Dst][HIGH], f.A[i.Src][HIGH])
			case 2:
				f.F[i.Dst][LOW] = fsub_2(f.F[i.Dst][LOW], f.A[i.Src][LOW])
				f.F[i.Dst][HIGH] = fsub_2(f.F[i.Dst][HIGH], f.A[i.Src][HIGH])
			case 3:
				f.F[i.Dst][LOW] = fsub_3(f.F[i.Dst][LOW], f.A[i.Src][LOW])
				f.F[i.Dst][HIGH] = fsub_3(f.F[i.Dst][HIGH], f.A[i.Src][HIGH])
			}
		case VM_FSUB_M:
			lo, hi := pad.Load32F(i.getScratchpadAddress(f.R[i.Src]))

			switch f.FPRC & 3 {
			case 0:
				f.F[i.Dst][LOW] = f.F[i.Dst][LOW] - lo
				f.F[i.Dst][HIGH] = f.F[i.Dst][HIGH] - hi
			case 1:
				f.F[i.Dst][LOW] = fsub_1(f.F[i.Dst][LOW], lo)
				f.F[i.Dst][HIGH] = fsub_1(f.F[i.Dst][HIGH], hi)
			case 2:
				f.F[i.Dst][LOW] = fsub_2(f.F[i.Dst][LOW], lo)
				f.F[i.Dst][HIGH] = fsub_2(f.F[i.Dst][HIGH], hi)
			case 3:
				f.F[i.Dst][LOW] = fsub_3(f.F[i.Dst][LOW], lo)
				f.F[i.Dst][HIGH] = fsub_3(f.F[i.Dst][HIGH], hi)
			}
		case VM_FSCAL_R:
			// no dependent on rounding modes
			f.F[i.Dst][LOW] = ScaleNegate(f.F[i.Dst][LOW])
			f.F[i.Dst][HIGH] = ScaleNegate(f.F[i.Dst][HIGH])
		case VM_FMUL_R:
			switch f.FPRC & 3 {
			case 0:
				f.E[i.Dst][LOW] = f.E[i.Dst][LOW] * f.A[i.Src][LOW]
				f.E[i.Dst][HIGH] = f.E[i.Dst][HIGH] * f.A[i.Src][HIGH]
			case 1:
				f.E[i.Dst][LOW] = fmul_fma_1(f.E[i.Dst][LOW], f.A[i.Src][LOW])
				f.E[i.Dst][HIGH] = fmul_fma_1(f.E[i.Dst][HIGH], f.A[i.Src][HIGH])
			case 2:
				f.E[i.Dst][LOW] = fmul_fma_2(f.E[i.Dst][LOW], f.A[i.Src][LOW])
				f.E[i.Dst][HIGH] = fmul_fma_2(f.E[i.Dst][HIGH], f.A[i.Src][HIGH])
			case 3:
				f.E[i.Dst][LOW] = fmul_fma_3(f.E[i.Dst][LOW], f.A[i.Src][LOW])
				f.E[i.Dst][HIGH] = fmul_fma_3(f.E[i.Dst][HIGH], f.A[i.Src][HIGH])
			}
		case VM_FDIV_M:
			lo, hi := pad.Load32F(i.getScratchpadAddress(f.R[i.Src]))
			lo = MaskRegisterExponentMantissa(lo, eMask[LOW])
			hi = MaskRegisterExponentMantissa(hi, eMask[HIGH])

			switch f.FPRC & 3 {
			case 0:
				f.E[i.Dst][LOW] = f.E[i.Dst][LOW] / lo
				f.E[i.Dst][HIGH] = f.E[i.Dst][HIGH] / hi
			case 1:
				f.E[i.Dst][LOW] = fdiv_fma_1(f.E[i.Dst][LOW], lo)
				f.E[i.Dst][HIGH] = fdiv_fma_1(f.E[i.Dst][HIGH], hi)
			case 2:
				f.E[i.Dst][LOW] = fdiv_fma_2(f.E[i.Dst][LOW], lo)
				f.E[i.Dst][HIGH] = fdiv_fma_2(f.E[i.Dst][HIGH], hi)
			case 3:
				f.E[i.Dst][LOW] = fdiv_fma_3(f.E[i.Dst][LOW], lo)
				f.E[i.Dst][HIGH] = fdiv_fma_3(f.E[i.Dst][HIGH], hi)
			}
		case VM_FSQRT_R:
			switch f.FPRC & 3 {
			case 0:
				f.E[i.Dst][LOW] = math.Sqrt(f.E[i.Dst][LOW])
				f.E[i.Dst][HIGH] = math.Sqrt(f.E[i.Dst][HIGH])
			case 1:
				f.E[i.Dst][LOW] = fsqrt_fma_1(f.E[i.Dst][LOW])
				f.E[i.Dst][HIGH] = fsqrt_fma_1(f.E[i.Dst][HIGH])
			case 2:
				f.E[i.Dst][LOW] = fsqrt_fma_2(f.E[i.Dst][LOW])
				f.E[i.Dst][HIGH] = fsqrt_fma_2(f.E[i.Dst][HIGH])
			case 3:
				f.E[i.Dst][LOW] = fsqrt_fma_3(f.E[i.Dst][LOW])
				f.E[i.Dst][HIGH] = fsqrt_fma_3(f.E[i.Dst][HIGH])
			}
		case VM_CFROUND:
			isrc := bits.RotateLeft64(f.R[i.Src], 0-int(i.Imm)) // rotate right
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

const lockThreadDueToRoundingMode = false

func SetRoundingMode(f *RegisterFile, mode uint8) {
	f.FPRC = mode
}

func ResetRoundingMode(f *RegisterFile) {
	f.FPRC = 0
}

//go:nosplit
func fadd_1(a, b float64) float64 {
	c := a + b
	return nextAfterNegativeFinite(sumResidue(a, b, c), c)
}

//go:nosplit
func fadd_2(a, b float64) float64 {
	c := a + b
	return nextAfterPositiveFinite(sumResidue(a, b, c), c)
}

//go:nosplit
func fadd_3(a, b float64) float64 {
	c := a + b
	return nextAfterZeroFinite(sumResidue(a, b, c), c)
}

//go:nosplit
func fsub_1(a, b float64) float64 {
	c := a - b
	return nextAfterNegativeFinite(sumResidue(a, -b, c), c)
}

//go:nosplit
func fsub_2(a, b float64) float64 {
	c := a - b
	return nextAfterPositiveFinite(sumResidue(a, -b, c), c)
}

//go:nosplit
func fsub_3(a, b float64) float64 {
	c := a - b
	return nextAfterZeroFinite(sumResidue(a, -b, c), c)
}

//go:nosplit
func sumResidue(a, b, c float64) float64 {
	deltaA := a - (c - b)
	deltaB := b - (c - a)
	return deltaA + deltaB
}

//go:nosplit
func mulResidueFMA(a, b, c float64) float64 {
	return math.FMA(a, b, -c)
}

//go:nosplit
func fmul_fma_1(a, b float64) float64 {
	c := a * b
	return nextAfterNegativeFinite(mulResidueFMA(a, b, c), c)
}

//go:nosplit
func fmul_fma_2(a, b float64) float64 {
	c := a * b
	return nextAfterPositiveFinite(mulResidueFMA(a, b, c), c)
}

//go:nosplit
func fmul_fma_3(a, b float64) float64 {
	c := a * b
	return nextAfterZeroNoZero(mulResidueFMA(a, b, c), c)
}

//go:nosplit
func fdiv_fma_1(a, b float64) float64 {
	c := a / b
	return nextAfterNegativeFinite(-mulResidueFMA(c, b, a), c)
}

//go:nosplit
func fdiv_fma_2(a, b float64) float64 {
	c := a / b
	return nextAfterPositiveFinite(-mulResidueFMA(c, b, a), c)
}

//go:nosplit
func fdiv_fma_3(a, b float64) float64 {
	c := a / b
	return nextAfterZeroNoZero(-mulResidueFMA(c, b, a), c)
}

//go:nosplit
func fsqrt_fma_1(a float64) float64 {
	c := math.Sqrt(a)
	return nextAfterNegativeFinite(-mulResidueFMA(c, c, a), c)
}

//go:nosplit
func fsqrt_fma_2(a float64) float64 {
	c := math.Sqrt(a)
	return nextAfterPositiveFinite(-mulResidueFMA(c, c, a), c)
}

//go:nosplit
func fsqrt_fma_3(a float64) float64 {
	c := math.Sqrt(a)
	return nextAfterZeroNoZero(-mulResidueFMA(c, c, a), c)
}

// mask64Bits returns 0xffffffffffffffff if cond is true, and 0 otherwise.
//
//go:nosplit
func mask64Bits(cond bool) uint64 {
	return ^(uint64(*(*uint8)(unsafe.Pointer(&cond))) - 1)
}

// mask1Bits returns 1 if cond is true, and 0 otherwise.
//
//go:nosplit
func mask1Bits(cond bool) uint64 {
	return uint64(*(*uint8)(unsafe.Pointer(&cond)))
}

//go:nosplit
func bitSelect(a, b uint64, mask uint64) uint64 {
	return (mask & a) | (^mask & b)
}

//go:nosplit
func nextAfterNegativeFinite(res, c float64) float64 {
	toRound := mask64Bits(res < 0)
	k := math.Float64bits(c) + (mask64Bits(c > 0) | 1)
	return math.Float64frombits(bitSelect(k, math.Float64bits(c), toRound))
}

//go:nosplit
func nextAfterPositiveFinite(res, c float64) float64 {
	toRound := mask64Bits(res > 0)
	k := math.Float64bits(c) + (mask64Bits(c < 0) | 1)
	return math.Float64frombits(bitSelect(k, math.Float64bits(c), toRound))
}

//go:nosplit
func nextAfterZeroFinite(res, c float64) float64 {
	return math.Float64frombits(math.Float64bits(c) - ((mask1Bits(res > 0) & mask1Bits(c < 0)) | (mask1Bits(res < 0) & mask1Bits(c > 0))))
}

//go:nosplit
func nextAfterZeroNoZero(res, c float64) float64 {
	return math.Float64frombits(math.Float64bits(c) - mask1Bits(res < 0))
}
