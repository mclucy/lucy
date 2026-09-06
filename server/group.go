package server

import (
	"fmt"
	"os/user"
	"runtime"
)

// EnsureAdminGroup creates the Lucy administration group if it is absent on
// Linux or macOS; debug dry-run skips all account changes.
func EnsureAdminGroup() error {
	if serviceDryRunEnabled() {
		return nil
	}
	if _, err := user.LookupGroup(DefaultGroup); err == nil {
		return nil
	}
	switch runtime.GOOS {
	case "linux":
		if err := runCommand("groupadd", "--system", DefaultGroup); err != nil {
			return fmt.Errorf("create %s group: %w", DefaultGroup, err)
		}
	case "darwin":
		if err := runCommand("dseditgroup", "-o", "create", DefaultGroup); err != nil {
			return fmt.Errorf("create %s group: %w", DefaultGroup, err)
		}
	default:
		return fmt.Errorf("automatic %s group creation is not supported on %s", DefaultGroup, runtime.GOOS)
	}
	return nil
}
