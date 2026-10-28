package cli

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/spf13/cobra"
)

// newStreamCommand returns a leaf command whose output streams are buffers.
func newStreamCommand() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "leaf"}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	return cmd, stdout, stderr
}

// selectJSON enables one of the machine-readable output switches on cmd.
func selectJSON(t *testing.T, cmd *cobra.Command, flag string) {
	t.Helper()
	cmd.Flags().Bool(flag, false, "Print machine-readable output")
	if err := cmd.Flags().Set(flag, "true"); err != nil {
		t.Fatalf("set --%s: %v", flag, err)
	}
}

func TestOutputRouting(t *testing.T) {
	t.Run("query writes to stdout", func(t *testing.T) {
		cmd, stdout, stderr := newStreamCommand()
		output := NewQueryOutput(cmd)
		if _, err := fmt.Fprint(output.Human(), "result\n"); err != nil {
			t.Fatalf("write human output: %v", err)
		}
		if got := stdout.String(); got != "result\n" {
			t.Fatalf("stdout = %q, want %q", got, "result\n")
		}
		if got := stderr.String(); got != "" {
			t.Fatalf("stderr = %q, want empty", got)
		}
	})

	t.Run("operation writes to stderr", func(t *testing.T) {
		cmd, stdout, stderr := newStreamCommand()
		output := NewOperationOutput(cmd)
		if _, err := fmt.Fprint(output.Human(), "installing\n"); err != nil {
			t.Fatalf("write human output: %v", err)
		}
		if got := stderr.String(); got != "installing\n" {
			t.Fatalf("stderr = %q, want %q", got, "installing\n")
		}
		if got := stdout.String(); got != "" {
			t.Fatalf("stdout = %q, want empty", got)
		}
	})

	t.Run("machine mode discards human output", func(t *testing.T) {
		cmd, stdout, stderr := newStreamCommand()
		selectJSON(t, cmd, FlagJSON)
		output := NewQueryOutput(cmd)
		if !output.JSON() {
			t.Fatal("output does not report machine mode")
		}
		if _, err := fmt.Fprint(output.Human(), "result\n"); err != nil {
			t.Fatalf("write human output: %v", err)
		}
		if got := stdout.String(); got != "" {
			t.Fatalf("stdout = %q, want empty", got)
		}
		if got := stderr.String(); got != "" {
			t.Fatalf("stderr = %q, want empty", got)
		}
	})

	t.Run("machine mode keeps warnings out of stderr", func(t *testing.T) {
		cmd, stdout, stderr := newStreamCommand()
		selectJSON(t, cmd, FlagJSONCompact)
		output := NewOperationOutput(cmd)
		output.Warn(errors.New("first warning"))
		output.Warn(errors.New("second warning"))
		output.Warn(nil)

		want := []string{"first warning", "second warning"}
		if got := output.Warnings(); !slices.Equal(got, want) {
			t.Fatalf("Warnings() = %q, want %q", got, want)
		}
		if got := stderr.String(); got != "" {
			t.Fatalf("stderr = %q, want empty", got)
		}
		if got := stdout.String(); got != "" {
			t.Fatalf("stdout = %q, want empty", got)
		}
	})
}

func TestWriteErrorUsesStderr(t *testing.T) {
	tests := []struct {
		name string
		flag string
	}{
		{name: "pretty", flag: FlagJSON},
		{name: "compact", flag: FlagJSONCompact},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd, stdout, stderr := newStreamCommand()
			selectJSON(t, cmd, test.flag)
			output := NewOperationOutput(cmd)
			output.WriteError(errors.New("boom"), "lucy.log")

			if got := stdout.String(); got != "" {
				t.Fatalf("stdout = %q, want empty", got)
			}

			// Unmarshal rejects trailing data, so this also proves stderr
			// carries exactly one document.
			var document ErrorDocument
			if err := json.Unmarshal(stderr.Bytes(), &document); err != nil {
				t.Fatalf("stderr is not one error document: %v (raw %q)", err, stderr.String())
			}
			if document.Schema != ErrorSchema {
				t.Fatalf("schema = %d, want %d", document.Schema, ErrorSchema)
			}
			if document.Kind != ErrorKind {
				t.Fatalf("kind = %q, want %q", document.Kind, ErrorKind)
			}
			if document.Code != CodeCommandFailed {
				t.Fatalf("code = %q, want %q", document.Code, CodeCommandFailed)
			}
			if document.Title != "boom" {
				t.Fatalf("title = %q, want %q", document.Title, "boom")
			}
			if document.Details == nil {
				t.Fatal("details is nil, want an encoded empty list")
			}
			if document.Log != "lucy.log" {
				t.Fatalf("log = %q, want %q", document.Log, "lucy.log")
			}
		})
	}
}
