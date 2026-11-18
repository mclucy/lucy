package cli

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/javaruntime"
	"github.com/spf13/cobra"
)

// newTestInvocation returns the presentation state a test command runs with.
func newTestInvocation() *Invocation {
	return NewInvocation(".", nil)
}

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
		output := NewQueryOutput(cmd, newTestInvocation())
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
		output := NewOperationOutput(cmd, newTestInvocation())
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
		output := NewQueryOutput(cmd, newTestInvocation())
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
		output := NewOperationOutput(cmd, newTestInvocation())
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
			output := NewOperationOutput(cmd, newTestInvocation())
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

// TestInstallRenderObservesWarningsOnce checks that a rendered install result
// reports its warnings to telemetry exactly once in every rendering, while the
// machine document and the human rendering keep them unchanged.
func TestInstallRenderObservesWarningsOnce(t *testing.T) {
	renderings := []struct {
		name string
		flag string
	}{
		{name: "text"},
		{name: "json", flag: FlagJSON},
		{name: "json compact", flag: FlagJSONCompact},
	}

	for _, rendering := range renderings {
		t.Run(rendering.name, func(t *testing.T) {
			cmd, stdout, stderr := newStreamCommand()
			if rendering.flag != "" {
				selectJSON(t, cmd, rendering.flag)
			}
			inv := newTestInvocation()
			observation := NewTelemetryObservation()
			inv.TelemetryObservation = observation
			output := newInstallOutput(cmd, inv)

			// A warning recorded while the operation ran is observed when it
			// is recorded, so rendering the result must not count it again.
			output.Warn(errors.New("earlier warning"))

			trace := output.Trace()
			trace.PhaseStarted("install")
			trace.PhaseDone(install.PhaseInfo{Name: "install", Count: 7})
			trace.ReportPhase(install.PhaseInfo{Name: "files", Count: 3})

			result := &install.Result{
				Warnings: []error{errors.New("result warning"), nil},
				Java: &javaruntime.Check{
					Source:   javaruntime.SourceConfigured,
					Problems: []string{"Java 17 is too old"},
				},
				Diagnostics: []filetrack.Diagnostic{{
					Path:    "server.jar",
					Message: "immutable deviation",
				}},
			}
			if err := output.Render(result); err != nil {
				t.Fatalf("render: %v", err)
			}

			_, _, scale, warnings := observation.Contexts(time.Now())
			scaleData, ok := scale.Data.(telemetryScaleData)
			if !ok {
				t.Fatalf("scale data = %T, want telemetryScaleData", scale.Data)
			}
			if scaleData.Files == nil || *scaleData.Files != 7 {
				t.Fatalf("files = %v, want 7", scaleData.Files)
			}
			warningData, ok := warnings.Data.(telemetryWarnings)
			if !ok {
				t.Fatalf("warnings data = %T, want telemetryWarnings", warnings.Data)
			}
			if warningData.Recoverable != 5 {
				t.Fatalf("recoverable warnings = %d, want 5", warningData.Recoverable)
			}

			if rendering.flag == "" {
				human := stderr.String()
				last := -1
				for _, want := range []string{
					"earlier warning",
					"result warning",
					"Java 17 is too old",
					"server.jar: immutable deviation",
				} {
					index := strings.Index(human, want)
					if index <= last {
						t.Fatalf("stderr %q does not report %q after the previous warning", human, want)
					}
					last = index
				}
				if got := stdout.String(); got != "" {
					t.Fatalf("stdout = %q, want empty", got)
				}
				return
			}

			var document struct {
				Warnings    []string               `json:"warnings"`
				Diagnostics []filetrack.Diagnostic `json:"diagnostics"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
				t.Fatalf("stdout is not one install document: %v (raw %q)", err, stdout.String())
			}
			want := []string{
				"result warning",
				"Java 17 is too old",
				"run lucy config set java.home <home> to select another Java runtime, " +
					"or lucy config set java.home '' to let Lucy choose",
				"earlier warning",
			}
			if !slices.Equal(document.Warnings, want) {
				t.Fatalf("warnings = %q, want %q", document.Warnings, want)
			}
			if len(document.Diagnostics) != 1 ||
				document.Diagnostics[0].Path != "server.jar" ||
				document.Diagnostics[0].Message != "immutable deviation" {
				t.Fatalf("diagnostics = %+v, want the file diagnostic", document.Diagnostics)
			}
			if got := stderr.String(); got != "" {
				t.Fatalf("stderr = %q, want empty", got)
			}
		})
	}
}

// TestObservePhaseCountAcceptsInstallAndFiles checks both phases whose count
// is a number of files: a committed installation and an imported workspace.
func TestObservePhaseCountAcceptsInstallAndFiles(t *testing.T) {
	tests := []struct {
		phase string
		count int
	}{
		{phase: "install", count: 7},
		{phase: "files", count: 3},
	}

	for _, test := range tests {
		t.Run(test.phase, func(t *testing.T) {
			observation := NewTelemetryObservation()
			observation.ObservePhaseCount(test.phase, test.count, 0)

			_, _, scale, _ := observation.Contexts(time.Now())
			scaleData, ok := scale.Data.(telemetryScaleData)
			if !ok {
				t.Fatalf("scale data = %T, want telemetryScaleData", scale.Data)
			}
			if scaleData.Files == nil || *scaleData.Files != test.count {
				t.Fatalf("files = %v, want %d", scaleData.Files, test.count)
			}
		})
	}
}
