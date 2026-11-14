//go:build 386 && !purego

package asm

//go:noescape
func setRoundingMode(mode uint8)
