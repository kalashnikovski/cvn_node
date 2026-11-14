//go:build amd64 && !purego

#include "textflag.h"

TEXT ·setRoundingMode(SB),NOSPLIT|NOFRAME,$8-1
	MOVB addr+0(FP), AX
	ANDQ $3, AX
	ROLQ $13, AX

    // get current MXCSR register
	PUSHQ AX
	STMXCSR 0(SP)

	// put new rounding mode
	ANDL $~0x6000, 0(SP)
	ORL AX, 0(SP)

	// store new MXCSR register
	LDMXCSR 0(SP)
	POPQ AX
	RET
