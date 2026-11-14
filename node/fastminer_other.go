//go:build !windows && !linux

package node

import "os/exec"

func prepareChildProcess(command *exec.Cmd) {}

func bindChildProcess(command *exec.Cmd) {}
