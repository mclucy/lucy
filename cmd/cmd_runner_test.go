package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mclucy/lucy/internal/cli"
)

// executeRunner runs one invocation the way main does: execute first, then
// publish any buffered machine result.
func executeRunner(t *testing.T, args []string, logPath string) (string, string, *Runner) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	runner := NewRunner(
		append(slices.Clone(args), "--log-file", logPath),
		RunnerOptions{
			Directory: filepath.Dir(logPath),
			Stdin:     strings.NewReader(""),
			Stdout:    &stdout,
			Stderr:    &stderr,
		},
	)
	if err := runner.Execute(t.Context()); err != nil {
		t.Fatalf("Execute(%v): %v", args, err)
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("Close(%v): %v", args, err)
	}
	if err := runner.FlushOutput(); err != nil {
		t.Fatalf("FlushOutput(%v): %v", args, err)
	}
	return stdout.String(), stderr.String(), runner
}

// decodeOneDocument asserts that data holds exactly one JSON value.
func decodeOneDocument(t *testing.T, data string, target any) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode %q: %v", data, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("output holds more than one JSON value: %q", data)
	}
}

func TestRunnerSchemaResult(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "lucy.log")

	stdout, stderr, runner := executeRunner(t, []string{"schema"}, logPath)
	if stderr != "" {
		t.Fatalf("stderr must stay clean, got %q", stderr)
	}
	if !runner.JSONSelected() {
		t.Fatal("schema must select machine output")
	}
	var listing any
	decodeOneDocument(t, stdout, &listing)

	stdout, stderr, runner = executeRunner(t, []string{"schema", "status"}, logPath)
	if stderr != "" {
		t.Fatalf("stderr must stay clean, got %q", stderr)
	}
	if !runner.JSONSelected() {
		t.Fatal("schema status must select machine output")
	}
	var document struct {
		Title string `json:"title"`
	}
	decodeOneDocument(t, stdout, &document)
	if !strings.Contains(strings.ToLower(document.Title), "status") {
		t.Fatalf("schema status title = %q, want it to mention status", document.Title)
	}
}

func TestRunnerFailureDocument(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "lucy.log")
	var stdout, stderr bytes.Buffer
	runner := NewRunner(
		[]string{"schema", "bogus", "--log-file", logPath},
		RunnerOptions{
			Directory: filepath.Dir(logPath),
			Stdin:     strings.NewReader(""),
			Stdout:    &stdout,
			Stderr:    &stderr,
		},
	)

	err := runner.Execute(t.Context())
	if closeErr := runner.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err == nil {
		t.Fatal("schema bogus must fail")
	}
	if !runner.JSONSelected() {
		t.Fatal("schema must select machine output for failures too")
	}
	// main.run renders the failure after Execute returns.
	runner.WriteError(err)

	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty, got %q", stdout.String())
	}
	var document struct {
		Schema int    `json:"schema"`
		Kind   string `json:"kind"`
		Code   string `json:"code"`
		Title  string `json:"title"`
	}
	decodeOneDocument(t, stderr.String(), &document)
	if document.Code != cli.CodeInvalidInvocation {
		t.Fatalf("code = %q, want %q", document.Code, cli.CodeInvalidInvocation)
	}
	if document.Kind != cli.ErrorKind {
		t.Fatalf("kind = %q, want %q", document.Kind, cli.ErrorKind)
	}
	if document.Title == "" {
		t.Fatal("failure document must carry a title")
	}
}

func TestRunnerIsolation(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "lucy.log")

	compactStdout, compactStderr, compactRunner := executeRunner(
		t,
		[]string{"schema", "--json-compact"},
		logPath,
	)
	prettyStdout, prettyStderr, prettyRunner := executeRunner(
		t,
		[]string{"schema", "--json-compact=false"},
		logPath,
	)

	if !compactRunner.JSONSelected() || !prettyRunner.JSONSelected() {
		t.Fatal("schema must select machine output in both invocations")
	}
	if compactStderr != "" || prettyStderr != "" {
		t.Fatalf("stderr must stay clean, got %q and %q", compactStderr, prettyStderr)
	}
	compact := strings.TrimSuffix(compactStdout, "\n")
	if strings.Contains(compact, "\n") {
		t.Fatalf("--json-compact output spans lines: %q", compactStdout)
	}
	pretty := strings.TrimSuffix(prettyStdout, "\n")
	if !strings.Contains(pretty, "\n") {
		t.Fatalf("a later invocation inherited compact output: %q", prettyStdout)
	}
	for _, output := range []string{compactStdout, prettyStdout} {
		var document any
		decodeOneDocument(t, output, &document)
	}
}

func TestRunnerAgentDefaults(t *testing.T) {
	t.Setenv("OPENCODE", "1")
	logPath := filepath.Join(t.TempDir(), "lucy.log")

	stdout, stderr, runner := executeRunner(t, []string{"schema"}, logPath)
	if stderr != "" {
		t.Fatalf("stderr must stay clean, got %q", stderr)
	}
	if !runner.JSONSelected() {
		t.Fatal("agent default must select machine output for a contract command")
	}
	compact := strings.TrimSuffix(stdout, "\n")
	if strings.Contains(compact, "\n") {
		t.Fatalf("agent default must be compact, got %q", stdout)
	}
	if !json.Valid([]byte(compact)) {
		t.Fatalf("agent default output is not valid JSON: %q", stdout)
	}
}
