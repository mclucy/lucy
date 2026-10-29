package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"
)

// nonTerminalInput is a stdin that no terminal backs, so a gate cannot open
// because of the host that runs the test.
func nonTerminalInput(t *testing.T) *os.File {
	t.Helper()
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

// promptProbeRoot builds a root whose only command reports the gate state.
func promptProbeRoot(t *testing.T, stdin *os.File, visit func(*cobra.Command)) *cobra.Command {
	t.Helper()

	root := &cobra.Command{Use: "lucy"}
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.PersistentFlags().Bool(FlagJSON, false, "Print JSON")
	root.PersistentFlags().Bool(FlagJSONCompact, false, "Print compact JSON")
	root.PersistentFlags().Bool(FlagNoInteractive, false, "Disable prompts")
	root.PersistentFlags().Bool(FlagNoStyle, false, "Disable styling")
	root.AddCommand(&cobra.Command{
		Use: "probe",
		RunE: func(cmd *cobra.Command, _ []string) error {
			visit(cmd)
			return nil
		},
	})
	root.SetIn(stdin)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return root
}

func TestPromptGateFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "plain", want: true},
		{name: "json", args: []string{"--json"}},
		{name: "compact", args: []string{"--json-compact"}},
		{name: "no interactive", args: []string{"--no-interactive"}},
		{name: "no style", args: []string{"--no-style"}},
		{
			name: "all switches",
			args: []string{
				"--json",
				"--json-compact",
				"--no-interactive",
				"--no-style",
			},
		},
	}

	if (PromptGate{}).flagsAllow() {
		t.Fatal("a gate without a command must not allow prompts")
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var allowed []bool
			root := promptProbeRoot(t, nonTerminalInput(t), func(cmd *cobra.Command) {
				allowed = append(allowed, NewPromptGate(cmd, newTestInvocation()).flagsAllow())
			})
			root.SetArgs(append([]string{"probe"}, test.args...))

			if err := root.Execute(); err != nil {
				t.Fatalf("execute probe: %v", err)
			}
			if len(allowed) != 1 {
				t.Fatalf("probe ran %d times, want 1", len(allowed))
			}
			if allowed[0] != test.want {
				t.Fatalf("flagsAllow() with %q = %t, want %t",
					test.args, allowed[0], test.want)
			}
		})
	}
}

// TestPromptGateWithoutTerminal pins the terminal half: the switches may
// allow prompting, but stdin that no terminal backs still denies it.
func TestPromptGateWithoutTerminal(t *testing.T) {
	var allowed, available bool
	root := promptProbeRoot(t, nonTerminalInput(t), func(cmd *cobra.Command) {
		inv := newTestInvocation()
		inv.Interactive = true
		gate := NewPromptGate(cmd, inv)
		allowed = gate.flagsAllow()
		available = gate.Available()
	})
	root.SetArgs([]string{"probe"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute probe: %v", err)
	}
	if !allowed {
		t.Fatal("plain switches must allow prompting")
	}
	if available {
		t.Fatal("a prompt gate opened on stdin that no terminal backs")
	}
}

func TestNormalizePromptError(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	unrelated := errors.New("network down")

	tests := []struct {
		name   string
		ctx    context.Context
		err    error
		wantIs error
	}{
		{name: "none", ctx: context.Background()},
		{
			name:   "cancelled",
			ctx:    context.Background(),
			err:    context.Canceled,
			wantIs: context.Canceled,
		},
		{
			name:   "aborted",
			ctx:    context.Background(),
			err:    huh.ErrUserAborted,
			wantIs: context.Canceled,
		},
		{
			name:   "context cause wins",
			ctx:    cancelled,
			err:    huh.ErrUserAborted,
			wantIs: context.Canceled,
		},
		{
			name:   "context wins over unrelated error",
			ctx:    cancelled,
			err:    errors.New("timeout"),
			wantIs: context.Canceled,
		},
		{
			name:   "unrelated error passes through",
			ctx:    context.Background(),
			err:    unrelated,
			wantIs: unrelated,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizePromptError(test.ctx, test.err)
			if test.wantIs == nil {
				if got != nil {
					t.Fatalf("NormalizePromptError() = %v, want nil", got)
				}
				return
			}
			if !errors.Is(got, test.wantIs) {
				t.Fatalf("NormalizePromptError() = %v, want %v", got, test.wantIs)
			}
		})
	}
}
