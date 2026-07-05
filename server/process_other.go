//go:build !unix && !darwin && !linux

package server

import (
	"os"
	"os/exec"
)

func prepareManagedProcess(cmd *exec.Cmd, runUser string) error {
	return applyRunUser(cmd, runUser)
}

func signalManagedProcess(cmd *exec.Cmd, signal os.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(signal)
}
