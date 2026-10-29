package cli

import (
	"context"

	"github.com/spf13/cobra"
)

// CommandContext derives the command's context. Signal handling belongs to
// the process boundary, which cancels the context the root was executed
// with; commands only propagate cancellation into their work.
func CommandContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithCancel(ctx)
}
