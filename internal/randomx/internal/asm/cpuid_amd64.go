//go:build amd64 && !purego

package asm

func Cpuid(op uint32) (eax, ebx, ecx, edx uint32)
