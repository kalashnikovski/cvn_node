//go:build !(desktop && windows)

package main

// RunWalletWindow does nothing in the miner and server builds. The desktop
// wallet is a separate build (see build_clean.bat).
func RunWalletWindow() bool {
	return false
}
