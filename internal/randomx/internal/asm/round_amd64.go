//go:build amd64 && !purego

package asm

//go:noescape
func setRoundingMode(mode uint8)
