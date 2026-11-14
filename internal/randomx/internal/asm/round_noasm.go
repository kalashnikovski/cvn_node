//go:build (!arm64 && !(arm.6 || arm.7) && !amd64 && !386) || purego

package asm

func setRoundingMode(mode uint8) {
	panic("not implemented")
}
