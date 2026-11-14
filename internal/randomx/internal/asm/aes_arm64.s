//go:build arm64 && !purego

#include "textflag.h"

#define aesenc(key, dst) \
	AESE V15.B16, dst \
	AESMC dst, dst \
	VEOR key, dst, dst

#define aesdec(key, dst) \
	AESD V15.B16, dst \
	AESIMC dst, dst \
	VEOR key, dst, dst

TEXT ·FillAes1Rx4(SB),NOSPLIT|NOFRAME,$0-32
	MOVD states+0(FP), R0
	MOVD keys+8(FP), R1
	MOVD output+16(FP), R2
	MOVD outputLen+24(FP), R3

    // initial state
	VLD1 (R0), [V0.B16, V1.B16, V2.B16, V3.B16]

    // keys: V4-V7
	VLD1 (R1), [V4.B16, V5.B16, V6.B16, V7.B16]

	// zero
	VEOR V15.B16, V15.B16, V15.B16

loop:

    aesdec(V4.B16, V0.B16)
    aesenc(V5.B16, V1.B16)
    aesdec(V6.B16, V2.B16)
    aesenc(V7.B16, V3.B16)

    // store state onto output
    VST1.P [V0.B16, V1.B16, V2.B16, V3.B16], 64(R2)

    // outputLen -= 64, continue if not 0
    SUB $64, R3
    CBNZ R3, loop

    // offload initial state
    VST1 [V0.B16, V1.B16, V2.B16, V3.B16], (R0)
	RET


TEXT ·HashAes1Rx4(SB),NOSPLIT|NOFRAME,$0-40
	MOVD initialState+0(FP), R0

    // initial state
	VLD1 (R0), [V0.B16, V1.B16, V2.B16, V3.B16]

	MOVD xKeys+8(FP), R0
	MOVD output+16(FP), R1
	MOVD input+24(FP), R2
	MOVD inputLen+32(FP), R3

	// zero
	VEOR V15.B16, V15.B16, V15.B16

loop:
    // input as keys: V4-V7
	VLD1.P 64(R2), [V4.B16, V5.B16, V6.B16, V7.B16]

    aesenc(V4.B16, V0.B16)
    aesdec(V5.B16, V1.B16)
    aesenc(V6.B16, V2.B16)
    aesdec(V7.B16, V3.B16)

    // inputLen -= 64, continue if not 0
    SUB $64, R3
    CBNZ R3, loop

    // do encdec1 with both keys!
	VLD1 (R0), [V4.B16, V5.B16]

    aesenc(V4.B16, V0.B16)
    aesdec(V4.B16, V1.B16)
    aesenc(V4.B16, V2.B16)
    aesdec(V4.B16, V3.B16)

    aesenc(V5.B16, V0.B16)
    aesdec(V5.B16, V1.B16)
    aesenc(V5.B16, V2.B16)
    aesdec(V5.B16, V3.B16)

    // offload into output
    VST1 [V0.B16, V1.B16, V2.B16, V3.B16], (R1)
	RET


TEXT ·MergeAes4Rx4(SB),NOSPLIT|NOFRAME,$0-16
	MOVD states+0(FP), R0
	MOVD key+8(FP), R1

	VLD1 (R0), [V0.B16, V1.B16, V2.B16, V3.B16]
	VLD1 (R1), [V4.B16, V5.B16, V6.B16, V7.B16]

	// zero
	VEOR V15.B16, V15.B16, V15.B16

    aesenc(V4.B16, V0.B16)
    aesdec(V4.B16, V1.B16)
    aesenc(V4.B16, V2.B16)
    aesdec(V4.B16, V3.B16)

    aesenc(V5.B16, V0.B16)
    aesdec(V5.B16, V1.B16)
    aesenc(V5.B16, V2.B16)
    aesdec(V5.B16, V3.B16)

    aesenc(V6.B16, V0.B16)
    aesdec(V6.B16, V1.B16)
    aesenc(V6.B16, V2.B16)
    aesdec(V6.B16, V3.B16)

    aesenc(V7.B16, V0.B16)
    aesdec(V7.B16, V1.B16)
    aesenc(V7.B16, V2.B16)
    aesdec(V7.B16, V3.B16)

    VST1 [V0.B16, V1.B16, V2.B16, V3.B16], (R0)
	RET

TEXT ·AESRoundTrip_DecEnc(SB),NOSPLIT|NOFRAME,$0-16
	MOVD states+0(FP), R0
	MOVD keys+8(FP), R1

	VLD1 (R0), [V0.B16, V1.B16, V2.B16, V3.B16]
    VLD1 (R1), [V4.B16, V5.B16, V6.B16, V7.B16]

	// zero
	VEOR V15.B16, V15.B16, V15.B16

    aesdec(V4.B16, V0.B16)
    aesenc(V5.B16, V1.B16)
    aesdec(V6.B16, V2.B16)
    aesenc(V7.B16, V3.B16)

    VST1 [V0.B16, V1.B16, V2.B16, V3.B16], (R0)
	RET
	

TEXT ·AESRoundTrip_EncDec(SB),NOSPLIT|NOFRAME,$0-16
	MOVD states+0(FP), R0
	MOVD keys+8(FP), R1

	VLD1 (R0), [V0.B16, V1.B16, V2.B16, V3.B16]
    VLD1 (R1), [V4.B16, V5.B16, V6.B16, V7.B16]

	// zero
	VEOR V15.B16, V15.B16, V15.B16

    aesenc(V4.B16, V0.B16)
    aesdec(V5.B16, V1.B16)
    aesenc(V6.B16, V2.B16)
    aesdec(V7.B16, V3.B16)

    VST1 [V0.B16, V1.B16, V2.B16, V3.B16], (R0)
	RET


TEXT ·AESRoundTrip_EncDec1(SB),NOSPLIT|NOFRAME,$0-16
	MOVD states+0(FP), R0
	MOVD key+8(FP), R1

	VLD1 (R0), [V0.B16, V1.B16, V2.B16, V3.B16]
	VLD1 (R1), [V4.B16]

	// zero
	VEOR V15.B16, V15.B16, V15.B16

    aesdec(V4.B16, V0.B16)
    aesenc(V4.B16, V1.B16)
    aesdec(V4.B16, V2.B16)
    aesenc(V4.B16, V3.B16)

    VST1 [V0.B16, V1.B16, V2.B16, V3.B16], (R0)
	RET

