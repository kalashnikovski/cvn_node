package randomx

import (
	"math"
	"math/bits"
)

const (
	mantbits64 uint = 52
	expbits64  uint = 11
)

const mantissaMask = (uint64(1) << mantbits64) - 1
const exponentMask = (uint64(1) << expbits64) - 1
const exponentBias = 1023
const dynamicExponentBits = 4
const staticExponentBits = 4
const constExponentBits uint64 = 0x300
const dynamicMantissaMask = (uint64(1) << (mantbits64 + dynamicExponentBits)) - 1

const mask22bit = (uint64(1) << 22) - 1

//go:nosplit
func MaskRegisterExponentMantissa(f float64, mode uint64) float64 {
	return math.Float64frombits((math.Float64bits(f) & dynamicMantissaMask) | mode)
}

//go:nosplit
func ScaleNegate(f float64) float64 {
	return math.Float64frombits(math.Float64bits(f) ^ 0x80F0000000000000)
}

func SmallPositiveFloatBits(entropy uint64) float64 {
	exponent := entropy >> 59 //0..31
	mantissa := entropy & mantissaMask
	exponent += exponentBias
	exponent &= exponentMask
	exponent = exponent << mantbits64
	return math.Float64frombits(exponent | mantissa)
}

func StaticExponent(entropy uint64) uint64 {
	exponent := constExponentBits
	exponent |= (entropy >> (64 - staticExponentBits)) << dynamicExponentBits
	exponent <<= mantbits64
	return exponent
}

func ExponentMask(entropy uint64) uint64 {
	return (entropy & mask22bit) | StaticExponent(entropy)
}

//go:nosplit
func Xor(a, b float64) float64 {
	return math.Float64frombits(math.Float64bits(a) ^ math.Float64bits(b))
}

//go:nosplit
func smulh(a, b int64) uint64 {
	hi_, _ := bits.Mul64(uint64(a), uint64(b))
	t1 := (a >> 63) & b
	t2 := (b >> 63) & a
	return uint64(int64(hi_) - t1 - t2)
}

// reciprocal
// Calculates rcp = 2**x / divisor for highest integer x such that rcp < 2**64.
// divisor must not be 0 or a power of 2
func reciprocal(divisor uint32) uint64 {

	const p2exp63 = uint64(1) << 63

	quotient := p2exp63 / uint64(divisor)
	remainder := p2exp63 % uint64(divisor)

	shift := bits.Len32(divisor)

	return (quotient << shift) + ((remainder << shift) / uint64(divisor))
}

func signExtend2sCompl(x uint32) uint64 {
	return uint64(int64(int32(x)))
}
