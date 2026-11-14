//go:build (windows || unix) && amd64 && !disable_jit && !purego

#include "textflag.h"

// func init_dataset_superscalar_run(itemNumber uint64, constants *[8]uint64, memory *[RANDOMX_ARGON_MEMORY]MemoryBlock, programs uintptr, rl *RegisterLine)
TEXT ·init_dataset_superscalar_run(SB),NOSPLIT|NOFRAME,$0-48

	MOVQ itemNumber+0(FP), AX
	MOVQ constants+8(FP), BX

    // rl.Init(itemNumber)

    // rl[0] = (itemNumber + 1) * keys.SuperScalar_Constants[0]
    ADDQ $1, AX
    MULQ 0(BX)
    MOVQ AX, R8

    // rl[1] = rl[0] ^ keys.SuperScalar_Constants[1]
    MOVQ 8(BX), R9
    XORQ R8, R9

    MOVQ 16(BX), R10
    XORQ R8, R10

    MOVQ 24(BX), R11
    XORQ R8, R11

    MOVQ 32(BX), R12
    XORQ R8, R12

    MOVQ 40(BX), R13
    XORQ R8, R13

    MOVQ 48(BX), R14
    XORQ R8, R14

    MOVQ 56(BX), R15
    XORQ R8, R15

    // registerValue / addr / mix
    MOVQ itemNumber+0(FP), BX

    // ptr to blocks / memory
    MOVQ memory+16(FP), DI

    MOVQ programs+24(FP), AX
    CALL AX

    MOVQ rl+32(FP), SI

    // move registers back to register line
    MOVQ R8, 0(SI)
    MOVQ R9, 8(SI)
    MOVQ R10, 16(SI)
    MOVQ R11, 24(SI)
    MOVQ R12, 32(SI)
    MOVQ R13, 40(SI)
    MOVQ R14, 48(SI)
    MOVQ R15, 56(SI)

    RET
