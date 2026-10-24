package cli

import (
	"context"
	"errors"
	"os"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

// PromptGate decides whether one command may open an interactive selection.
// Machine-readable output and --no-interactive (which --no-style implies) turn
// prompts off: neither can be steered by terminal input. A confirmation flag
// such as --yes does not belong here because it answers a question rather
// than selecting the value the prompt would have selected.
type PromptGate struct{ cmd *cobra.Command }

func NewPromptGate(cmd *cobra.Command) PromptGate {
	return PromptGate{cmd: cmd}
}

// Available reports whether a prompt may read the terminal. A caller that
// still needs the selection fails with the flag that carries the same choice.
func (g PromptGate) Available() bool {
	if g.cmd == nil || JSONMode(g.cmd) != JSONNone {
		return false
	}
	if noInteractive, _ := g.cmd.Flags().GetBool(FlagNoInteractive); noInteractive {
		return false
	}
	if noStyle, _ := g.cmd.Flags().GetBool(FlagNoStyle); noStyle {
		return false
	}
	stdin, ok := g.cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(stdin.Fd()) && style.InteractiveEnabled()
}

// Huh can report a cancelled context as a timeout instead of its cause.
func NormalizePromptError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if cause := ctx.Err(); cause != nil {
		return cause
	}
	if errors.Is(err, huh.ErrUserAborted) {
		return context.Canceled
	}
	return err
}
