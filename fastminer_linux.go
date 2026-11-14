//go:build linux

package main

import (
	"os/exec"
	"syscall"
)

// The fast miner must never outlive this program. On Linux the system is
// asked to stop it whenever this program ends, for any reason.

func prepareChildProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}

func bindChildProcess(command *exec.Cmd) {}
