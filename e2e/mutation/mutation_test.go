//go:build e2e

// Package mutation records heuristic CLI outcomes and checks established contracts.
package mutation

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mclucy/lucy/e2e/internal/testkit"
	"go.yaml.in/yaml/v4"
)

const (
	envLayer     = "LUCY_HEURISTIC_LAYER"
	envReportDir = "LUCY_HEURISTIC_REPORT_DIR"

	envCommandTimeout = "LUCY_HEURISTIC_COMMAND_TIMEOUT"

	envSample = "LUCY_HEURISTIC_SAMPLE"
	envSeed   = "LUCY_HEURISTIC_SEED"

	defaultCommandTimeout = 3 * time.Minute
	// waitDelay bounds how long output pipes may stay open after the process
	// exits or is killed.
	waitDelay = 2 * time.Second

	// processTreeDrainTimeout bounds how long a killed process tree may take to
	// empty. Termination takes effect asynchronously.
	processTreeDrainTimeout = 10 * time.Second

	manifestName   = "lucy.yaml"
	lockName       = "lucy-lock.yaml"
	statusExited   = "exited"
	statusSignaled = "signaled"
	statusTimeout  = "timeout"
	statusSkipped  = "skipped"
)

// commandTimeout returns the bound on one CLI invocation. envCommandTimeout
// takes a Go duration such as "90s" or "10m". Unset or blank selects
// defaultCommandTimeout. A value that does not parse, or is not positive,
// fails the test.
func commandTimeout(t *testing.T) time.Duration {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(envCommandTimeout))
	if raw == "" {
		return defaultCommandTimeout
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil {
		t.Fatalf("%s %q: want a Go duration such as 90s or 10m: %v", envCommandTimeout, raw, err)
	}
	if timeout <= 0 {
		t.Fatalf("%s %q: want a positive duration", envCommandTimeout, raw)
	}
	return timeout
}

var caseIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// upgradeValueFlags are the upgrade and global flags that consume the next
// token. That token is a flag value, not a positional package target.
var upgradeValueFlags = []string{"--policy", "--log-level", "--log-file", "--server"}

type fixtureCase struct {
	ID       string            `yaml:"id"`
	Smoke    bool              `yaml:"smoke"`
	Manifest *string           `yaml:"manifest"`
	Args     argumentList      `yaml:"args"`
	Setup    argumentList      `yaml:"setup"`
	Files    map[string]string `yaml:"files"`
}

// argumentList records whether its key was present, so an omitted key differs
// from an empty list.
type argumentList struct {
	values []string
	set    bool
}

func (a *argumentList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d: must be a sequence", node.Line)
	}
	if err := node.Decode(&a.values); err != nil {
		return err
	}
	a.set = true
	return nil
}

// cliResult holds what one CLI invocation produced.
type cliResult struct {
	outcome runOutcome
	stdout  []byte
	stderr  []byte
}

func TestAddHeuristic(t *testing.T) {
	runHeuristic(t, "add")
}

func TestRemoveHeuristic(t *testing.T) {
	runHeuristic(t, "remove")
}

func TestUpgradeAllHeuristic(t *testing.T) {
	runHeuristic(t, "upgrade")
}

func runHeuristic(t *testing.T, command string) {
	binary := testkit.Lucy(t)
	layer := selectedLayer(t)
	timeout := commandTimeout(t)
	t.Logf("%s command timeout %s", command, timeout)
	raw, err := os.ReadFile(filepath.Join("testdata", command+".yaml"))
	if err != nil {
		t.Fatalf("read %s fixtures: %v", command, err)
	}
	cases := decodeFixtures(t, command, raw)
	selected, sample := resolveSample(t, command, selectCases(t, cases, layer))
	run := newReportRun(t, command, raw, layer, sample)
	for _, c := range selected {
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel()
			executeCase(t, binary, run, c)
		})
	}
}

func selectedLayer(t *testing.T) string {
	t.Helper()
	switch layer := os.Getenv(envLayer); layer {
	case "", "full":
		return "full"
	case "smoke":
		return "smoke"
	default:
		t.Fatalf("unknown %s %q: want smoke or full", envLayer, layer)
		return ""
	}
}

func decodeFixtures(t *testing.T, command string, raw []byte) []fixtureCase {
	t.Helper()
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var cases []fixtureCase
	if err := decoder.Decode(&cases); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("parse %s fixtures: %v", command, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("%s fixtures must hold exactly one YAML sequence", command)
	}
	if err := validateFixtures(command, cases); err != nil {
		t.Fatalf("%s fixtures: %v", command, err)
	}
	return cases
}

func validateFixtures(command string, cases []fixtureCase) error {
	if len(cases) == 0 {
		return errors.New("no cases")
	}
	ids := make(map[string]bool, len(cases))
	for index, c := range cases {
		if !caseIDPattern.MatchString(c.ID) {
			return fmt.Errorf("case %d: id %q is not kebab-case", index, c.ID)
		}
		if ids[c.ID] {
			return fmt.Errorf("duplicate id %q", c.ID)
		}
		ids[c.ID] = true
		if c.Manifest == nil {
			return fmt.Errorf("%s: manifest is required (use an empty string for an empty file)", c.ID)
		}
		if !c.Args.set {
			return fmt.Errorf("%s: args is required", c.ID)
		}
		if err := validateArgs(command, c.Args.values); err != nil {
			return fmt.Errorf("%s: %w", c.ID, err)
		}
		if err := validateSetup(c.Setup); err != nil {
			return fmt.Errorf("%s: %w", c.ID, err)
		}
		if err := validateFiles(c.Files); err != nil {
			return fmt.Errorf("%s: %w", c.ID, err)
		}
	}
	return nil
}

func validateArgs(command string, args []string) error {
	if command == "upgrade" {
		return validateUpgradeArgs(args)
	}
	if len(args) == 0 {
		return fmt.Errorf("%s needs at least one argument", command)
	}
	return nil
}

// validateSetup requires an absent key or a list whose first word names a
// command. An empty list is rejected because it cannot run anything.
func validateSetup(setup argumentList) error {
	if !setup.set {
		return nil
	}
	if len(setup.values) == 0 || strings.HasPrefix(setup.values[0], "-") {
		return errors.New("setup must start with a command name")
	}
	return nil
}

// validateUpgradeArgs rejects positional tokens. Flag values listed in
// upgradeValueFlags are skipped, and everything after "--" is positional.
func validateUpgradeArgs(args []string) error {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--":
			if index+1 < len(args) {
				return errors.New("upgrade takes no positional arguments")
			}
			return nil
		case arg == "-" || !strings.HasPrefix(arg, "-"):
			return fmt.Errorf("upgrade takes no positional arguments, found %q", arg)
		case slices.Contains(upgradeValueFlags, arg):
			if index+1 >= len(args) {
				return fmt.Errorf("%s needs a value", arg)
			}
			index++
		}
	}
	return nil
}

func validateFiles(files map[string]string) error {
	paths := slices.Sorted(maps.Keys(files))
	for _, p := range paths {
		if err := validateFixturePath(p); err != nil {
			return fmt.Errorf("file %q: %w", p, err)
		}
	}
	// Paths are sorted, so a directory always precedes the files beneath it.
	for index, parent := range paths {
		for _, child := range paths[index+1:] {
			if strings.HasPrefix(child, parent+"/") {
				return fmt.Errorf("file %q conflicts with directory %q", parent, child)
			}
		}
	}
	return nil
}

func validateFixturePath(p string) error {
	switch {
	case p == "" || p == ".":
		return errors.New("path must name a file")
	case strings.EqualFold(strings.SplitN(p, "/", 2)[0], manifestName):
		return errors.New("manifest content belongs in the manifest field")
	case strings.ContainsAny(p, `\:`):
		return errors.New("path must use forward slashes and no volume names")
	case strings.HasPrefix(p, "/"):
		return errors.New("path must be relative")
	case path.Clean(p) != p || slices.Contains(strings.Split(p, "/"), ".."):
		return errors.New("path must be clean and stay inside the workspace")
	case !filepath.IsLocal(filepath.FromSlash(p)):
		return errors.New("path escapes the workspace")
	}
	return nil
}

func selectCases(t *testing.T, cases []fixtureCase, layer string) []fixtureCase {
	t.Helper()
	if layer == "full" {
		return cases
	}
	var selected []fixtureCase
	for _, c := range cases {
		if c.Smoke {
			selected = append(selected, c)
		}
	}
	if len(selected) == 0 {
		t.Fatalf("no smoke cases selected")
	}
	return selected
}

// resolveSample applies LUCY_HEURISTIC_SAMPLE and LUCY_HEURISTIC_SEED to one
// operation's layer-filtered cases. Each operation resolves on its own cases, so
// an omitted seed is drawn separately per operation. Without a sample count the
// cases pass through unchanged.
func resolveSample(t *testing.T, command string, selected []fixtureCase) ([]fixtureCase, *sampleIndex) {
	t.Helper()
	rawCount := strings.TrimSpace(os.Getenv(envSample))
	rawSeed := strings.TrimSpace(os.Getenv(envSeed))
	if rawCount == "" {
		if rawSeed != "" {
			t.Fatalf("%s needs %s: a seed only applies to a sample", envSeed, envSample)
		}
		return selected, nil
	}
	requested, err := strconv.ParseUint(rawCount, 10, 64)
	switch {
	case err != nil:
		t.Fatalf("%s %q: want a positive decimal count: %v", envSample, rawCount, err)
	case requested == 0:
		t.Fatalf("%s %q: want a positive decimal count", envSample, rawCount)
	case requested > uint64(len(selected)):
		t.Fatalf("%s %d: %s has only %d selected cases", envSample, requested, command, len(selected))
	}
	count := int(requested)
	seed := sampleSeed(t, rawSeed)
	chosen := sampleCases(selected, count, seed)
	ids := make([]string, len(chosen))
	for i, c := range chosen {
		ids[i] = c.ID
	}
	seedText := strconv.FormatUint(seed, 10)
	t.Logf("%s sample: %d of %d selected cases, seed %s", command, count, len(selected), seedText)
	t.Logf("%s sampled cases: %s", command, strings.Join(ids, " "))
	return chosen, &sampleIndex{
		Count:      count,
		Population: len(selected),
		Seed:       seedText,
		IDs:        ids,
	}
}

// sampleSeed parses an explicit seed, or draws one from crypto/rand when raw is
// empty. The caller logs and records the seed so the run can be reproduced.
func sampleSeed(t *testing.T, raw string) uint64 {
	t.Helper()
	if raw != "" {
		seed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			t.Fatalf("%s %q: want an unsigned 64-bit decimal: %v", envSeed, raw, err)
		}
		return seed
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("draw sample seed: %v", err)
	}
	return binary.LittleEndian.Uint64(buf[:])
}

// sampleCases draws count distinct cases with a partial Fisher-Yates shuffle and
// returns them in fixture order.
func sampleCases(selected []fixtureCase, count int, seed uint64) []fixtureCase {
	order := make([]int, len(selected))
	for i := range order {
		order[i] = i
	}
	gen := sampleGenerator{state: seed}
	for i := range count {
		j := i + int(gen.below(uint64(len(order)-i)))
		order[i], order[j] = order[j], order[i]
	}
	picked := make([]fixtureCase, count)
	for i, index := range slices.Sorted(slices.Values(order[:count])) {
		picked[i] = selected[index]
	}
	return picked
}

// sampleGenerator is splitmix64 with fixed arithmetic. Its output depends only on
// the seed, not on the standard library's derived-method algorithms, so a
// recorded seed reproduces the same selection later.
type sampleGenerator struct {
	state uint64
}

func (g *sampleGenerator) next() uint64 {
	g.state += 0x9e3779b97f4a7c15
	z := g.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// below returns a uniform value in [0, n). Values below 2^64 mod n are rejected
// so that no residue is favored.
func (g *sampleGenerator) below(n uint64) uint64 {
	threshold := -n % n
	for {
		if v := g.next(); v >= threshold {
			return v % n
		}
	}
}

// The input snapshot is taken before setup, and the report is persisted before
// the temporary workspace is removed.
func executeCase(t *testing.T, binary string, run *reportRun, c fixtureCase) {
	t.Helper()
	workspace, environment := newIsolatedWorkspace(t)
	writeWorkspace(t, workspace, c)
	before, err := snapshotTree(workspace)
	if err != nil {
		t.Fatalf("snapshot input workspace: %v", err)
	}
	caseDir := run.caseDir(t, c.ID)
	if err := copyTree(workspace, filepath.Join(caseDir, "input")); err != nil {
		t.Fatalf("persist input workspace: %v", err)
	}
	report := caseReport{
		Command: run.command,
		ID:      c.ID,
		Smoke:   c.Smoke,
		Fixture: run.fixture,
		Definition: caseDefinition{
			Manifest: *c.Manifest,
			Args:     c.Args.values,
			Setup:    c.Setup.values,
			Files:    c.Files,
		},
		Invocation: invocation{
			Binary:    binary,
			Directory: "workspace",
			Timeout:   commandTimeout(t).String(),
		},
		Before: before,
	}
	var runs []stageRun
	previous := before
	setupSucceeded := true
	if c.Setup.set {
		setup := runStage(t, "setup", binary, commandLine(c.Setup.values), workspace, environment, previous)
		runs = append(runs, setup)
		report.Setup = &setup.record
		setupSucceeded = succeeded(setup.record.Outcome)
		previous = *setup.record.After
	}
	words := append([]string{run.command}, c.Args.values...)
	var main stageRun
	if setupSucceeded {
		main = runStage(t, "command", binary, commandLine(words), workspace, environment, previous)
	} else {
		main = skippedStage("command", commandLine(words), "setup did not succeed; command not run")
	}
	runs = append(runs, main)
	report.Main = main.record
	if err := persistCase(caseDir, report, runs); err != nil {
		t.Fatalf("persist case report: %v", err)
	}
	entry := indexEntry{
		ID:       c.ID,
		Smoke:    c.Smoke,
		Status:   main.record.Outcome.Status,
		ExitCode: main.record.Outcome.ExitCode,
		Report:   filepath.ToSlash(filepath.Join("cases", c.ID, "report.json")),
	}
	if report.Setup != nil {
		entry.SetupStatus = report.Setup.Outcome.Status
		entry.SetupExitCode = report.Setup.Outcome.ExitCode
	}
	if err := run.record(entry); err != nil {
		t.Fatalf("persist run index: %v", err)
	}
	t.Logf("%s: command %s, report %s", c.ID, main.record.Outcome.Status, filepath.Join(caseDir, "report.json"))
}

// commandLine places --no-style directly after the command word, so fixture
// arguments never precede the runner's flag.
func commandLine(words []string) []string {
	return slices.Concat(words[:1], []string{"--no-style"}, words[1:])
}

func succeeded(outcome runOutcome) bool {
	return outcome.Status == statusExited && outcome.ExitCode != nil && *outcome.ExitCode == 0
}

func failedWithExit(outcome runOutcome) bool {
	return outcome.Status == statusExited && outcome.ExitCode != nil && *outcome.ExitCode != 0
}

func runStage(
	t *testing.T,
	name, binary string,
	argv []string,
	workspace string,
	environment []string,
	previous snapshot,
) stageRun {
	t.Helper()
	result := runCLI(t, binary, argv, workspace, environment)
	after, err := snapshotTree(workspace)
	if err != nil {
		t.Fatalf("snapshot %s workspace: %v", name, err)
	}
	changes := diffSnapshots(previous, after)
	stdout := newFileRecord(name+"-stdout.txt", result.stdout)
	stderr := newFileRecord(name+"-stderr.txt", result.stderr)
	return stageRun{
		record: stage{
			Name:    name,
			Argv:    argv,
			Outcome: result.outcome,
			Stdout:  &stdout,
			Stderr:  &stderr,
			After:   &after,
			Changes: &changes,
		},
		stdout: result.stdout,
		stderr: result.stderr,
	}
}

// skippedStage records a stage that did not run, so its absence is never
// mistaken for an execution.
func skippedStage(name string, argv []string, reason string) stageRun {
	return stageRun{record: stage{
		Name:    name,
		Argv:    argv,
		Outcome: runOutcome{Status: statusSkipped, Detail: reason},
	}}
}

func newIsolatedWorkspace(t *testing.T) (string, []string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	temporary := filepath.Join(root, "tmp")
	workspace := filepath.Join(root, "workspace")
	for _, dir := range []string{home, temporary, workspace} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	return workspace, isolatedEnvironment(home, temporary)
}

func writeWorkspace(t *testing.T, workspace string, c fixtureCase) {
	t.Helper()
	writeFileAt(t, filepath.Join(workspace, manifestName), *c.Manifest)
	for _, p := range slices.Sorted(maps.Keys(c.Files)) {
		writeFileAt(t, filepath.Join(workspace, filepath.FromSlash(p)), c.Files[p])
	}
}

func writeFileAt(t *testing.T, target, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("create directory for %s: %v", target, err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", target, err)
	}
}

// isolatedEnvironment drops inherited variables that could reach
// machine-global Lucy or git state, or that mark a coding-agent harness, and
// points user-scoped locations inside the case root. Telemetry is off.
func isolatedEnvironment(home, temporary string) []string {
	inherited := slices.DeleteFunc(os.Environ(), isolatedVariable)
	return append(inherited,
		"HOME="+home,
		"USERPROFILE="+home,
		"APPDATA="+filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA="+filepath.Join(home, "AppData", "Local"),
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"TMPDIR="+temporary,
		"TMP="+temporary,
		"TEMP="+temporary,
		"LUCY_TELEMETRY=off",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"TERM=dumb",
	)
}

func isolatedVariable(entry string) bool {
	name, _, _ := strings.Cut(entry, "=")
	name = strings.ToUpper(name)
	switch name {
	case "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TMPDIR", "TMP", "TEMP", "TERM",
		"OPENCODE", "QWEN_CODE_SESSION_ID", "PI_CODING_AGENT", "AI_AGENT", "OMPCODE",
		"PI_SESSION_FILE", "PI_SESSION_ID", "DSH_SHELL", "CURSOR_AGENT", "CLAUDECODE",
		"CODEX_THREAD_ID", "AMP_CURRENT_THREAD_ID", "GEMINI_CLI", "AUGMENT_AGENT", "CRUSH",
		"COPILOT_AGENT", "OZ_RUN_ID":
		return true
	}
	for _, prefix := range []string{"XDG_", "LUCY_", "GIT_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// runCLI runs the binary with argv in dir under commandTimeout. Only a failure
// to start the process fails the test; every other ending becomes the returned
// outcome.
func runCLI(t *testing.T, binary string, argv []string, dir string, environment []string) cliResult {
	t.Helper()
	return runProcess(t, commandTimeout(t), binary, argv, dir, environment)
}

// runProcess runs binary with argv in dir under timeout, then stops the process
// tree it started; see startProcessTree. Output pipes can stay open after the
// leader exits, so the tree is stopped on every ending, before the outcome is
// classified. A canceled test context is a harness interruption, not a timeout.
func runProcess(t *testing.T, timeout time.Duration, binary string, argv []string, dir string, environment []string) cliResult {
	t.Helper()
	parent := t.Context()
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, binary, argv...)
	command.Dir = dir
	command.Env = environment
	command.WaitDelay = waitDelay
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	started := time.Now()
	var interrupted atomic.Bool
	tree, err := startProcessTree(command, &interrupted)
	if err != nil {
		t.Fatalf("start %s %v: %v", binary, argv, err)
	}
	runErr := command.Wait()
	timedOut := interrupted.Load() && errors.Is(ctx.Err(), context.DeadlineExceeded)
	outcome := runOutcome{DurationMS: time.Since(started).Milliseconds()}
	killed, stopErr := tree.stop()
	if stopErr != nil {
		t.Errorf("%s %v: %v", binary, argv, stopErr)
	}
	if parentErr := parent.Err(); parentErr != nil {
		t.Fatalf("run %s %v interrupted: %v", binary, argv, parentErr)
	}
	state := command.ProcessState
	if state == nil {
		t.Fatalf("wait %s %v: %v", binary, argv, runErr)
	}
	switch {
	case timedOut:
		outcome.Status = statusTimeout
		outcome.Detail = "killed after " + timeout.String()
		if state.Exited() {
			code := state.ExitCode()
			outcome.ExitCode = &code
		}
	case runErr == nil, errors.Is(runErr, exec.ErrWaitDelay), errors.As(runErr, new(*exec.ExitError)):
		var notes []string
		if errors.Is(runErr, exec.ErrWaitDelay) {
			notes = append(notes, "output pipes stayed open after exit")
		}
		if killed {
			notes = append(notes, "leftover processes killed")
		}
		recordProcessState(&outcome, state, strings.Join(notes, "; "))
	default:
		t.Fatalf("run %s %v: %v", binary, argv, runErr)
	}
	return cliResult{outcome: outcome, stdout: stdout.Bytes(), stderr: stderr.Bytes()}
}

func recordProcessState(outcome *runOutcome, state *os.ProcessState, detail string) {
	if state.Exited() {
		code := state.ExitCode()
		outcome.Status = statusExited
		outcome.ExitCode = &code
	} else {
		outcome.Status = statusSignaled
		detail = state.String()
	}
	outcome.Detail = detail
}

// The contract anchors below run one fixed offline-safe command per case and
// check its observable effect on the workspace. They fail on a timeout, on a
// clean exit, and on a nonzero exit whose output does not name the expected
// reason, since none of those prove the contract.

// Reasons the anchors require in a rejected command's output. Each names the
// failure the fixed command must reach, so an unrelated early exit cannot
// satisfy an anchor.
const (
	reasonUnparseableManifest = "decode manifest:"
	reasonMissingLock         = lockName + " is required"
)

// toolDirectory is the directory Lucy takes its workspace lock in before it
// reads documents. A rejected command may leave it behind.
const toolDirectory = ".lucy"

func requireRejection(t *testing.T, binary string, argv []string, workspace string, environment []string, reasons ...string) {
	t.Helper()
	result := runCLI(t, binary, argv, workspace, environment)
	if !failedWithExit(result.outcome) {
		t.Fatalf("%v: want a nonzero exit, got %+v\nstdout:\n%s\nstderr:\n%s",
			argv, result.outcome, result.stdout, result.stderr)
	}
	output := normalizedOutput(result)
	for _, reason := range reasons {
		if !strings.Contains(output, reason) {
			t.Fatalf("%v: exited %d without reason %q\nstdout:\n%s\nstderr:\n%s",
				argv, *result.outcome.ExitCode, reason, result.stdout, result.stderr)
		}
	}
}

// normalizedOutput joins both streams with single spaces, so a reason the
// terminal wrapped across lines still matches.
func normalizedOutput(result cliResult) string {
	return strings.Join(strings.Fields(string(result.stdout)+" "+string(result.stderr)), " ")
}

func requireBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s changed:\nwant:\n%s\ngot:\n%s", path, want, got)
	}
}

func requireAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s must not exist: %v", path, err)
	}
}

// requireWorkspaceEntries fails unless the workspace holds exactly the want
// paths, given workspace-relative and slash-separated. The toolDirectory
// subtree is skipped.
func requireWorkspaceEntries(t *testing.T, workspace string, want ...string) {
	t.Helper()
	var got []string
	err := filepath.WalkDir(workspace, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(workspace, p)
		if err != nil {
			return err
		}
		switch {
		case rel == ".":
			return nil
		case rel == toolDirectory && entry.IsDir():
			return filepath.SkipDir
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", workspace, err)
	}
	wanted := slices.Sorted(slices.Values(want))
	slices.Sort(got)
	if !slices.Equal(got, wanted) {
		t.Errorf("workspace entries: want %q, got %q", wanted, got)
	}
}

// validManifest is a complete vanilla manifest without packages. Upgrade
// reads it and then fails on the missing lock, not on the manifest.
const validManifest = `manifest_version: 1
server:
  minecraft: "1.20.1"
  directory: server
  core:
    product: minecraft
    product_version: "1.20.1"
`

func TestMutationAddRejectsMissingManifest(t *testing.T) {
	t.Parallel()
	binary := testkit.Lucy(t)
	workspace, environment := newIsolatedWorkspace(t)
	requireRejection(t, binary, []string{"add", "--no-style", "--offline", "modrinth:lithium"},
		workspace, environment, manifestName, osMissingFileText)
	requireAbsent(t, filepath.Join(workspace, manifestName))
	requireAbsent(t, filepath.Join(workspace, lockName))
	requireWorkspaceEntries(t, workspace)
}

func TestMutationRemoveRejectsMissingManifest(t *testing.T) {
	t.Parallel()
	binary := testkit.Lucy(t)
	workspace, environment := newIsolatedWorkspace(t)
	requireRejection(t, binary, []string{"remove", "--no-style", "--offline", "modrinth:lithium"},
		workspace, environment, manifestName, osMissingFileText)
	requireAbsent(t, filepath.Join(workspace, manifestName))
	requireAbsent(t, filepath.Join(workspace, lockName))
	requireWorkspaceEntries(t, workspace)
}

func TestMutationRemoveKeepsUnparseableManifest(t *testing.T) {
	t.Parallel()
	binary := testkit.Lucy(t)
	workspace, environment := newIsolatedWorkspace(t)
	const unparseable = "manifest_version: 1\nserver: [unclosed\n"
	writeFileAt(t, filepath.Join(workspace, manifestName), unparseable)
	requireRejection(t, binary, []string{"remove", "--no-style", "--offline", "modrinth:lithium"},
		workspace, environment, reasonUnparseableManifest)
	requireBytes(t, filepath.Join(workspace, manifestName), []byte(unparseable))
	requireAbsent(t, filepath.Join(workspace, lockName))
	requireWorkspaceEntries(t, workspace, manifestName)
}

func TestMutationUpgradeKeepsManifestWithoutLock(t *testing.T) {
	t.Parallel()
	binary := testkit.Lucy(t)
	workspace, environment := newIsolatedWorkspace(t)
	writeFileAt(t, filepath.Join(workspace, manifestName), validManifest)
	requireRejection(t, binary, []string{"upgrade", "--no-style"}, workspace, environment, reasonMissingLock)
	requireBytes(t, filepath.Join(workspace, manifestName), []byte(validManifest))
	requireAbsent(t, filepath.Join(workspace, lockName))
	requireWorkspaceEntries(t, workspace, manifestName)
}
