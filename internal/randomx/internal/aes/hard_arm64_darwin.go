//go:build darwin && arm64 && !purego

package aes

const HasHardAESImplementation = true

// SupportsHardAESImplementation Assume all M1+ have AES
//
// See https://github.com/golang/go/issues/43046
// See https://github.com/golang/go/commit/c15593197453b8bf90fc3a9080ba2afeaf7934ea
var SupportsHardAESImplementation = true
