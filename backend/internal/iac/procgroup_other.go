//go:build !unix

package iac

import "os/exec"

func newProcessGroup(cmd *exec.Cmd) {}

func stopProcessGroup(cmd *exec.Cmd, kill bool) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
