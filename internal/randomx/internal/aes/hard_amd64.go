//go:build amd64 && !purego

package aes

import (
	"golang.org/x/sys/cpu"
)

const HasHardAESImplementation = true

var SupportsHardAESImplementation = cpu.X86.HasAES
