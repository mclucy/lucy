//go:build !linux && !darwin

package cli

import (
	"fmt"
	"os/exec"
)

// awaitManagedExit disables raw process-group signals where no non-reaping
// exit observer is available; os.Process.Signal still protects direct signals.
func awaitManagedExit(_ *exec.Cmd) error {
	return fmt.Errorf("non-reaping exit observation unavailable")
}
