//go:build 386 && !purego

#include "textflag.h"

TEXT ·setRoundingMode(SB),NOSPLIT|NOFRAME,$4-1
	MOVB addr+0(FP), AX
	ANDL $3, AX
	ROLL $13, AX

    // get current MXCSR register
	PUSHL AX
	STMXCSR 0(SP)

	// put new rounding mode
	ANDL $~0x6000, 0(SP)
	ORL AX, 0(SP)

	// store new MXCSR register
	LDMXCSR 0(SP)
	POPL AX
	RET
