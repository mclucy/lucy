package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

// PromptGate decides whether one command may open an interactive selection.
// Machine-readable output and --no-interactive (which --no-style implies) turn
// prompts off: neither can be steered by terminal input.
type PromptGate struct {
	cmd *cobra.Command
	inv *Invocation
}

func NewPromptGate(cmd *cobra.Command, inv *Invocation) PromptGate {
	return PromptGate{cmd: cmd, inv: inv}
}

// Input is the stream a prompt may read from.
func (g PromptGate) Input() io.Reader { return g.cmd.InOrStdin() }

// Output is the stream a prompt renders on. Prompts never own stdout.
func (g PromptGate) Output() io.Writer { return g.cmd.ErrOrStderr() }

// Available reports whether a prompt may read the terminal. A caller that
// still needs the selection fails with the flag that carries the same choice.
func (g PromptGate) Available() bool {
	if !g.flagsAllow() || !g.inv.Interactive {
		return false
	}
	stdin, ok := g.cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(stdin.Fd())
}

// flagsAllow reports whether the command's switches leave prompting enabled.
// Keeping it apart from the terminal test makes the switch behavior verifiable
// without a terminal.
func (g PromptGate) flagsAllow() bool {
	if g.cmd == nil || JSONMode(g.cmd) != JSONNone {
		return false
	}
	if noInteractive, _ := g.cmd.Flags().GetBool(FlagNoInteractive); noInteractive {
		return false
	}
	if noStyle, _ := g.cmd.Flags().GetBool(FlagNoStyle); noStyle {
		return false
	}
	return true
}

type promptTimerKey struct{}

// PromptTimer accumulates the observed wait of the forms one invocation ran.
// Elapsed is unmeasured until a form executes, so an unopened prompt differs
// from a form that closed immediately.
type PromptTimer struct {
	mu       sync.Mutex
	elapsed  time.Duration
	measured bool
}

// WithPromptTimer attaches the invocation's input timer to its form context.
func WithPromptTimer(ctx context.Context, timer *PromptTimer) context.Context {
	return context.WithValue(ctx, promptTimerKey{}, timer)
}

func (t *PromptTimer) measure(elapsed time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.elapsed += elapsed
	t.measured = true
	t.mu.Unlock()
}

func (t *PromptTimer) wait() (time.Duration, bool) {
	if t == nil {
		return 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.elapsed, t.measured
}

// RunPrompt runs one form and measures its wait when the invocation requests
// it. The measurement covers each form exactly once, even when it fails.
func RunPrompt(ctx context.Context, form *huh.Form) error {
	timer, _ := ctx.Value(promptTimerKey{}).(*PromptTimer)
	if timer == nil {
		return NormalizePromptError(ctx, form.RunWithContext(ctx))
	}
	started := time.Now()
	raw := form.RunWithContext(ctx)
	timer.measure(time.Since(started))
	return NormalizePromptError(ctx, raw)
}

// ErrPromptAborted reports an explicit form abort. It wraps context.Canceled
// so every caller that treats a canceled invocation as an interruption keeps
// matching the abort.
var ErrPromptAborted = fmt.Errorf("prompt aborted: %w", context.Canceled)

// Huh can report a cancelled context as a timeout instead of its cause.
func NormalizePromptError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if cause := ctx.Err(); cause != nil {
		return cause
	}
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrPromptAborted
	}
	return err
}
