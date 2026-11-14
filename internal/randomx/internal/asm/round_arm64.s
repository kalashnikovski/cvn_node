//go:build arm64 && !purego

#include "textflag.h"

TEXT ·getFPCR(SB),NOSPLIT,$0-8
	MOVD FPCR, R1
	MOVD R1, value+0(FP)
	RET

TEXT ·setFPCR(SB),NOSPLIT,$0-8
	MOVD value+0(FP), R1
	MOVD R1, FPCR
	RET
