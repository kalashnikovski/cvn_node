//go:build amd64 && !purego

#include "textflag.h"

// func Cpuid(op uint32) (eax, ebx, ecx, edx uint32)
TEXT ·Cpuid(SB), 7, $0
    XORQ CX, CX
    MOVL op+0(FP), AX
    CPUID
    MOVL AX, eax+8(FP)
    MOVL BX, ebx+12(FP)
    MOVL CX, ecx+16(FP)
    MOVL DX, edx+20(FP)
    RET

