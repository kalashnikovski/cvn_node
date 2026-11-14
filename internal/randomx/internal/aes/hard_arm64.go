//go:build !darwin && arm64 && !purego

package aes

import (
	"golang.org/x/sys/cpu"
)

const HasHardAESImplementation = true

var SupportsHardAESImplementation = cpu.ARM64.HasAES
