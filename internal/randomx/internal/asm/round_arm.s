//go:build (arm.6 || arm.7) && !purego

#include "textflag.h"

TEXT ·getFPSCR(SB),NOSPLIT,$0-4
	WORD	$0xeef1ba10	// vmrs r11, fpscr
	MOVW R11, value+0(FP)
	RET

TEXT ·setFPSCR(SB),NOSPLIT,$0-4
	MOVW value+0(FP), R11
	WORD	$0xeee1ba10	// vmsr fpscr, r11
	RET
