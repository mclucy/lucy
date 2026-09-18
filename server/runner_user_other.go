//go:build !unix && !darwin && !linux

package server

import (
	"fmt"
	"os/exec"
	"runtime"
)

// applyRunUser rejects an explicit run user on platforms without credential
// switching; an empty run user leaves the child under the caller's identity.
func applyRunUser(_ *exec.Cmd, runUser string) error {
	if runUser != "" {
		return fmt.Errorf("running as user %q is not supported on %s", runUser, runtime.GOOS)
	}
	return nil
}
