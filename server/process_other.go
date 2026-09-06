//go:build !unix && !darwin && !linux

package server

import (
	"os"
	"os/exec"
)

// prepareManagedProcess validates run-user handling on platforms where Lucy
// does not configure a separate child process group.
func prepareManagedProcess(cmd *exec.Cmd, runUser string) error {
	return applyRunUser(cmd, runUser)
}

// signalManagedProcess signals only the child process on this platform and
// treats a command without a started process as a no-op.
func signalManagedProcess(cmd *exec.Cmd, signal os.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(signal)
}
