//go:build !unix

package javainstall

import "os"

// interruptCommand kills process because the platform does not deliver
// interrupts to another process. Run still waits for the direct process to
// exit.
func interruptCommand(process *os.Process) error {
	return process.Kill()
}

// commandExitUnconfirmed reports false because this platform describes
// process termination with exit codes instead of signals.
func commandExitUnconfirmed(*os.ProcessState, bool) bool {
	return false
}
