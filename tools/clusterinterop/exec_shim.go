package main

import "os/exec"

func newExec(binary string, args ...string) *exec.Cmd {
	return exec.Command(binary, args...)
}
