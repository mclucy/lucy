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
	"github.com/spf13/cobra"
)

func TestCommandTreeContracts(t *testing.T) {
	contracts := cli.NewJSONContracts()
	root := NewRootCommand(contracts)

	// Commands that intentionally expose only human output. Every other
	// runnable command must declare a JSON result contract.
	humanOnly := map[string]bool{
		"compile": true,
		"docs":    true,
		"doctor":  true,
		"export":  true,
	}
	// Container commands render help; their children carry the contracts.
	helpOnly := map[string]bool{
		"bisect": true,
		"cache":  true,
		"file":   true,
		"java":   true,
	}

	// An allowlist must name a live root command, so a rename cannot silently
	// leave a command exempt.
	rootChild := func(name string) *cobra.Command {
		for _, child := range root.Commands() {
			if child.Name() == name {
				return child
			}
		}
		return nil
	}
	for name := range humanOnly {
		child := rootChild(name)
		if child == nil {
			t.Errorf("humanOnly names unknown root command %q", name)
			continue
		}
		if !child.Runnable() {
			t.Errorf("humanOnly names non-runnable command %q", name)
		}
		if contracts.Supports(child) {
			t.Errorf("%q is human-only but declares a JSON result", name)
		}
	}
	for name := range helpOnly {
		child := rootChild(name)
		if child == nil {
			t.Errorf("helpOnly names unknown root command %q", name)
			continue
		}
		if !child.Runnable() || !child.HasSubCommands() {
			t.Errorf("helpOnly names %q, want a runnable container", name)
		}
		if contracts.Supports(child) {
			t.Errorf("%q renders only help but declares a JSON result", name)
		}
	}

	var runnable int
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, child := range parent.Commands() {
			walk(child)
			if !child.Runnable() {
				continue
			}
			runnable++
			if contracts.Supports(child) {
				continue
			}
			if humanOnly[topLevelName(root, child)] {
				continue
			}
			if child.Parent() == root && helpOnly[child.Name()] && child.HasSubCommands() {
				continue
			}
			t.Errorf("%s is runnable but declares no JSON result contract",
				child.CommandPath())
		}
	}
	walk(root)
	if runnable == 0 {
		t.Fatal("command tree exposes no runnable commands")
	}

	docsCmd, remaining, err := root.Find([]string{"docs"})
	if err != nil || len(remaining) != 0 {
		t.Fatalf("find docs: command=%v remaining=%v err=%v",
			docsCmd, remaining, err)
	}
	if !docsCmd.Runnable() {
		t.Fatal("docs must stay runnable")
	}
	if !contracts.Ignores(docsCmd) {
		t.Fatal("docs must be ignored by the JSON contracts")
	}
	if contracts.Supports(docsCmd) {
		t.Fatal("docs must not declare a JSON result")
	}

	paths := contracts.Paths(root)
	if len(paths) == 0 {
		t.Fatal("command tree declares no JSON result paths")
	}
	if !slices.IsSorted(paths) {
		t.Fatalf("Paths must be sorted, got %v", paths)
	}
	for _, path := range paths {
		found, extra, err := root.Find(strings.Fields(path))
		if err != nil || len(extra) != 0 {
			t.Errorf("declared path %q does not resolve: remaining=%v err=%v",
				path, extra, err)
			continue
		}
		if !contracts.Supports(found) {
			t.Errorf("declared path %q resolves to an unsupported command", path)
		}
		schema, err := contracts.Schema(found)
		if err != nil {
			t.Errorf("schema for declared path %q: %v", path, err)
			continue
		}
		if schema == nil {
			t.Errorf("schema for declared path %q is nil", path)
		}
	}
}

// topLevelName reports the root-level command that owns cmd.
func topLevelName(root, cmd *cobra.Command) string {
	for current := cmd; current != nil; current = current.Parent() {
		if current.Parent() == root {
			return current.Name()
		}
	}
	return cmd.Name()
}

// executeRunner runs one invocation the way main does: execute first, then
// publish any buffered machine result.
func executeRunner(t *testing.T, args []string, logPath string) (string, string, *Runner) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	runner := NewRunner(
		append(slices.Clone(args), "--log-file", logPath),
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err := runner.Execute(); err != nil {
		t.Fatalf("Execute(%v): %v", args, err)
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
		strings.NewReader(""),
		&stdout,
		&stderr,
	)

	err := runner.Execute()
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

	firstRoot := NewRootCommand(cli.NewJSONContracts())
	secondRoot := NewRootCommand(cli.NewJSONContracts())
	if firstRoot == secondRoot {
		t.Fatal("NewRootCommand must build a fresh tree per call")
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
