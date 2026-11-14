//go:build !(amd64 || arm64) || purego

package aes

const HasHardAESImplementation = false

var SupportsHardAESImplementation = false

// HardAES alias done to keep types
type HardAES = SoftAES

func NewHardAES() *HardAES {
	return nil
}
