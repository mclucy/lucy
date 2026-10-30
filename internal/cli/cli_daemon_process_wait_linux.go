package cli

import (
	"os/exec"

	"golang.org/x/sys/unix"
)

// awaitManagedExit observes a child exit without freeing its process identifier.
func awaitManagedExit(cmd *exec.Cmd) error {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if err != unix.EINTR {
			return err
		}
	}
}
