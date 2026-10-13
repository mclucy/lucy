package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NotImplementedRun answers commands whose behavior is not designed yet.
//
// An advertised command must fail loudly instead of succeeding silently;
// silence reads as a completed operation.
func NotImplementedRun(cmd *cobra.Command, _ []string) error {
	return fmt.Errorf("%s: not implemented yet", cmd.Name())
}
