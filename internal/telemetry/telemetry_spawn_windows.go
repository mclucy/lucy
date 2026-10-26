//go:build windows

package telemetry

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureDetached(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}

func spawnUploader(path string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, "telemetry", "upload", path)
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	command.Env = os.Environ()
	configureDetached(command)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
