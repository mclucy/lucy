//go:build unix

package javainstall

import (
	"os"
	"syscall"
)

// interruptCommand asks process to stop. The signal comes from a user
// process, so sudo relays it to the command it runs and waits for that
// command to exit.
func interruptCommand(process *os.Process) error {
	return process.Signal(os.Interrupt)
}

// commandExitUnconfirmed reports whether the process ended from a signal
// that does not confirm it waited for a child. Only the SIGINT expected
// after a canceled context is treated as the relayed cancellation; any other
// signal may have bypassed that wait.
func commandExitUnconfirmed(state *os.ProcessState, canceled bool) bool {
	if state == nil {
		return false
	}
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return false
	}
	return !canceled || status.Signal() != syscall.SIGINT
}
