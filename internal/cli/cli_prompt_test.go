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

func TestPromptGateDenials(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "plain", args: nil},
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

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var opened []bool

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
					opened = append(opened, NewPromptGate(cmd).Available())
					return nil
				},
			})
			root.SetIn(nonTerminalInput(t))
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			root.SetArgs(append([]string{"probe"}, test.args...))

			if err := root.Execute(); err != nil {
				t.Fatalf("execute probe: %v", err)
			}
			if len(opened) != 1 {
				t.Fatalf("probe ran %d times, want 1", len(opened))
			}
			if opened[0] {
				t.Fatalf("prompt gate opened with %q", test.args)
			}
		})
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
