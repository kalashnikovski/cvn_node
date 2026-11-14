//go:build (arm.6 || arm.7) && !purego

package asm

// GetFPSCR returns the value of FPSCR register.
func getFPSCR() (value uint32)

// SetFPSCR writes the FPSCR value.
func setFPSCR(value uint32)

func setRoundingMode(mode uint8) {
	switch mode {
	// switch plus/minus infinity
	case 1:
		mode = 2
	case 2:
		mode = 1

	}
	fpscr := getFPSCR()
	fpscr = (fpscr & (^uint32(0x0C00000))) | ((uint32(mode) & 3) << 22)
	setFPSCR(fpscr)
}
