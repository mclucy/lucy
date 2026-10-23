package cli

import (
	"os"

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
