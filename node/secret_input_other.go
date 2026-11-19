//go:build !windows

package node

import (
	"os"
	"os/exec"
)

// hideTyping stops the terminal showing what is typed, for a private key or
// a password. It returns a function that puts the terminal back as it was,
// and whether it could hide anything.
func hideTyping() (restore func(), hidden bool) {
	hide := exec.Command("stty", "-echo")
	hide.Stdin = os.Stdin
	if hide.Run() != nil {
		return func() {}, false
	}
	return func() {
		show := exec.Command("stty", "echo")
		show.Stdin = os.Stdin
		_ = show.Run()
	}, true
}
