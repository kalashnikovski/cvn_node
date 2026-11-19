//go:build windows

package node

import (
	"os"
	"syscall"
)

// hideTyping stops the console showing what is typed, for a private key or
// a password. It returns a function that puts the console back as it was,
// and whether it could hide anything: input that is not a console (a pipe,
// a file) has nothing to switch off.
func hideTyping() (restore func(), hidden bool) {
	handle := syscall.Handle(os.Stdin.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(handle, &mode); err != nil {
		return func() {}, false
	}
	setConsoleMode := syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")
	const showTyping = 0x0004 // ENABLE_ECHO_INPUT
	if done, _, _ := setConsoleMode.Call(uintptr(handle), uintptr(mode&^showTyping)); done == 0 {
		return func() {}, false
	}
	return func() { _, _, _ = setConsoleMode.Call(uintptr(handle), uintptr(mode)) }, true
}
