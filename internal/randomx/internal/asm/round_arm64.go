//go:build arm64 && !purego

package asm

// GetFPCR returns the value of FPCR register.
func getFPCR() (value uint32)

// SetFPCR writes the FPCR value.
func setFPCR(value uint32)

func setRoundingMode(mode uint8) {
	switch mode {
	// switch plus/minus infinity
	case 1:
		mode = 2
	case 2:
		mode = 1

	}
	fpcr := getFPCR()
	fpcr = (fpcr & (^uint32(0x0C00000))) | ((uint32(mode) & 3) << 22)
	setFPCR(fpcr)
}
