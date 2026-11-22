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

// TestInstallRenderProjectsCounts checks that a rendered install result keeps
// the observed file and package counts in the execution projection while
// every warning still reaches each rendering once.
func TestInstallRenderProjectsCounts(t *testing.T) {
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

			// A warning recorded while the operation ran is reported when it
			// is recorded, so rendering the result must not repeat it.
			output.Warn(errors.New("earlier warning"))

			trace := output.Trace()
			trace.PackageCount(0)
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

			execution := observedExecution(t, observation)
			if execution.Files == nil || *execution.Files != 7 {
				t.Fatalf("files = %v, want 7", execution.Files)
			}
			if packages := observation.Packages(); packages == nil || *packages != 0 {
				t.Fatalf("packages = %v, want an observed zero", packages)
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

// observedExecution projects the execution context of one observation and
// returns its bounded data.
func observedExecution(t *testing.T, observation *TelemetryObservation) TelemetryExecution {
	t.Helper()
	context := observation.Execution(time.Now())
	data, ok := context.Data.(TelemetryExecution)
	if !ok {
		t.Fatalf("execution data = %T, want TelemetryExecution", context.Data)
	}
	return data
}

// TestExecutionProjectsObservedCounts checks that files and packages keep a
// real zero and stay optional when no phase observed them.
func TestExecutionProjectsObservedCounts(t *testing.T) {
	t.Run("unobserved", func(t *testing.T) {
		observation := NewTelemetryObservation()
		if files := observedExecution(t, observation).Files; files != nil {
			t.Fatalf("files = %d, want unobserved", *files)
		}
		if packages := observation.Packages(); packages != nil {
			t.Fatalf("packages = %d, want unobserved", *packages)
		}
	})

	t.Run("zero", func(t *testing.T) {
		observation := NewTelemetryObservation()
		observation.ObserveFiles(0)
		observation.ObservePackages(0)
		if files := observedExecution(t, observation).Files; files == nil || *files != 0 {
			t.Fatalf("files = %v, want an observed zero", files)
		}
		if packages := observation.Packages(); packages == nil || *packages != 0 {
			t.Fatalf("packages = %v, want an observed zero", packages)
		}
	})
}

// TestExecutionPhasesUseCodeNames covers a completed and an unfinished phase
// whose names are normalized from operation code.
func TestExecutionPhasesUseCodeNames(t *testing.T) {
	observation := NewTelemetryObservation()
	observation.PhaseStart("python environment")
	observation.PhaseDone("python environment", 2*time.Second)
	observation.PhaseStart("loader-installer")

	execution := observedExecution(t, observation)
	phases := map[string]TelemetryPhase{}
	for _, phase := range execution.Phases {
		phases[phase.Name] = phase
	}
	complete, ok := phases["python_environment"]
	if !ok || complete.State != "complete" || complete.ElapsedMS != 2000 {
		t.Fatalf("completed phase = %+v, want python_environment complete at 2000 ms", complete)
	}
	active, ok := phases["loader-installer"]
	if !ok || active.State != "active" {
		t.Fatalf("active phase = %+v, want loader-installer active", active)
	}
	if execution.ActivePhase != "loader-installer" {
		t.Fatalf("active phase = %q, want loader-installer", execution.ActivePhase)
	}
}

// TestExecutionKeepsActivePhaseAtCap checks that a phase past the projection
// cap never replaces the active entry it could not record.
func TestExecutionKeepsActivePhaseAtCap(t *testing.T) {
	observation := NewTelemetryObservation()
	for index := range maxTelemetryPhaseCount {
		observation.PhaseStart(fmt.Sprintf("phase-%d", index))
	}
	last := fmt.Sprintf("phase-%d", maxTelemetryPhaseCount-1)
	observation.PhaseStart("overflow")

	execution := observedExecution(t, observation)
	if execution.ActivePhase != last {
		t.Fatalf("active phase = %q, want %q", execution.ActivePhase, last)
	}
	for _, phase := range execution.Phases {
		if phase.Name == "overflow" {
			t.Fatal("an overflow phase was recorded past the cap")
		}
	}
}

// TestExecutionInputWaitIsOptional checks that an unopened form leaves the
// wait unmeasured, a form that closed immediately reports zero, and the
// accumulated wait stays inside the wire bound.
func TestExecutionInputWaitIsOptional(t *testing.T) {
	observation := NewTelemetryObservation()
	if wait := observedExecution(t, observation).InputWaitMS; wait != nil {
		t.Fatalf("input wait = %d ms, want unmeasured", *wait)
	}

	observation.promptTimer.measure(0)
	if wait := observedExecution(t, observation).InputWaitMS; wait == nil {
		t.Fatal("input wait is unmeasured, want an observed zero")
	} else if *wait != 0 {
		t.Fatalf("input wait = %d ms, want 0", *wait)
	}

	observation.promptTimer.measure(1500 * time.Millisecond)
	if wait := observedExecution(t, observation).InputWaitMS; wait == nil {
		t.Fatal("input wait is unmeasured, want 1500 ms")
	} else if *wait != 1500 {
		t.Fatalf("input wait = %d ms, want 1500 ms", *wait)
	}

	observation.promptTimer.measure(8 * 24 * time.Hour)
	if wait := observedExecution(t, observation).InputWaitMS; wait == nil {
		t.Fatal("input wait is unmeasured, want the bound")
	} else if *wait != maxTelemetryElapsedMS {
		t.Fatalf("input wait = %d ms, want the %d ms bound", *wait, maxTelemetryElapsedMS)
	}
}
