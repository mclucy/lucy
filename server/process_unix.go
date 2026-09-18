//go:build unix || darwin || linux

package server

import (
	"os"
	"os/exec"
	"syscall"
)

// prepareManagedProcess gives the child its own process group for group-wide
// shutdown and configures its run user before the command is started.
func prepareManagedProcess(cmd *exec.Cmd, runUser string) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return applyRunUser(cmd, runUser)
}

// signalManagedProcess signals the child's process group when available,
// otherwise its process; a command without a started process is a no-op.
func signalManagedProcess(cmd *exec.Cmd, signal os.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	sysSignal, ok := signal.(syscall.Signal)
	if !ok {
		return cmd.Process.Signal(signal)
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		return syscall.Kill(-pgid, sysSignal)
	}
	return cmd.Process.Signal(signal)
}
