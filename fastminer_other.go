//go:build !windows && !linux

package main

import "os/exec"

func prepareChildProcess(command *exec.Cmd) {}

func bindChildProcess(command *exec.Cmd) {}
