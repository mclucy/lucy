//go:build e2e

package mutation

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mclucy/lucy/e2e/internal/testkit"
)

// Larger files keep only their size and digest, so binaries are never loaded.
const inlineTextLimit = 1 << 20

// LUCY_HEURISTIC_REPORT_DIR wins. Otherwise Go's artifact directory is used
// when -artifacts is set, because Go deletes that directory after the test
// when the flag is absent. The fallback sits under the git-ignored test_
// prefix so reports persist across runs.
func reportRoot(t *testing.T) string {
	t.Helper()
	if value := os.Getenv(envReportDir); value != "" {
		absolute, err := filepath.Abs(value)
		if err != nil {
			t.Fatalf("resolve %s: %v", envReportDir, err)
		}
		return absolute
	}
	if artifactsEnabled() {
		return t.ArtifactDir()
	}
	return filepath.Join(testkit.RepoRoot(t), "test_e2e-reports", "mutation")
}

func artifactsEnabled() bool {
	value := flag.Lookup("test.artifacts")
	return value != nil && value.Value.String() == "true"
}

type fileRecord struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func newFileRecord(path string, data []byte) fileRecord {
	sum := sha256.Sum256(data)
	return fileRecord{
		Path:   filepath.ToSlash(path),
		Size:   int64(len(data)),
		SHA256: hex.EncodeToString(sum[:]),
	}
}

type reportRun struct {
	command string
	fixture fileRecord
	layer   string
	sample  *sampleIndex
	dir     string

	// mu serializes index writes. They share one temporary file name, so
	// concurrent writers would clobber each other.
	mu      sync.Mutex
	entries []indexEntry
}

type indexEntry struct {
	ID            string `json:"id"`
	Smoke         bool   `json:"smoke"`
	Status        string `json:"status"`
	ExitCode      *int   `json:"exit_code,omitzero"`
	SetupStatus   string `json:"setup_status,omitempty"`
	SetupExitCode *int   `json:"setup_exit_code,omitzero"`
	Report        string `json:"report"`
}

type runIndex struct {
	Command string       `json:"command"`
	Layer   string       `json:"layer"`
	Fixture fileRecord   `json:"fixture"`
	Sample  *sampleIndex `json:"sample,omitzero"`
	Cases   []indexEntry `json:"cases"`
}

// Seed is decimal text because JSON readers that parse numbers as float64 lose
// precision above 2^53.
type sampleIndex struct {
	Count       int      `json:"count"`
	Population  int      `json:"population"`
	Seed        string   `json:"seed"`
	IDs         []string `json:"ids"`
	NotExecuted []string `json:"not_executed,omitempty"`
}

// The index is written before any case runs, so an interrupted run still shows
// what it selected.
func newReportRun(
	t *testing.T,
	command string,
	fixtureBytes []byte,
	layer string,
	sample *sampleIndex,
) *reportRun {
	t.Helper()
	runID, err := newRunID()
	if err != nil {
		t.Fatalf("create report run id: %v", err)
	}
	commandDir := filepath.Join(reportRoot(t), command)
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatalf("create report directory: %v", err)
	}
	dir := filepath.Join(commandDir, runID)
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			t.Fatalf("report run %s already exists; refusing to overwrite it", dir)
		}
		t.Fatalf("create report run: %v", err)
	}
	run := &reportRun{
		command: command,
		fixture: newFileRecord(filepath.Join("fixture", command+".yaml"), fixtureBytes),
		layer:   layer,
		sample:  sample,
		dir:     dir,
	}
	if err := writeFile(filepath.Join(dir, filepath.FromSlash(run.fixture.Path)), fixtureBytes); err != nil {
		t.Fatalf("persist fixture bytes: %v", err)
	}
	run.mu.Lock()
	err = run.writeIndexLocked()
	run.mu.Unlock()
	if err != nil {
		t.Fatalf("write run index: %v", err)
	}
	t.Logf("%s heuristic reports: %s", command, dir)
	t.Cleanup(func() { run.finish(t) })
	return run
}

// newRunID combines a UTC timestamp with random bytes. Two runs in the same
// nanosecond still differ.
func newRunID() (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	return stamp + "-" + hex.EncodeToString(suffix[:]), nil
}

// Failing when it already exists keeps earlier reports intact.
func (r *reportRun) caseDir(t *testing.T, id string) string {
	t.Helper()
	casesDir := filepath.Join(r.dir, "cases")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatalf("create cases directory: %v", err)
	}
	dir := filepath.Join(casesDir, id)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("create case report %s: %v", dir, err)
	}
	return dir
}

// Rewriting the index on every record keeps it current if the run is
// interrupted.
func (r *reportRun) record(entry indexEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
	return r.writeIndexLocked()
}

func (r *reportRun) finish(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.writeIndexLocked(); err != nil {
		t.Errorf("write run index: %v", err)
		return
	}
	if r.sample == nil {
		return
	}
	if missing := r.sampleSummary(r.entries).NotExecuted; len(missing) > 0 {
		t.Logf("%s sampled cases with no completed record: %s", r.command, strings.Join(missing, " "))
	}
}

// The caller holds r.mu.
func (r *reportRun) writeIndexLocked() error {
	entries := append([]indexEntry{}, r.entries...)
	slices.SortFunc(entries, func(a, b indexEntry) int {
		return strings.Compare(a.ID, b.ID)
	})
	index := runIndex{
		Command: r.command,
		Layer:   r.layer,
		Fixture: r.fixture,
		Sample:  r.sampleSummary(entries),
		Cases:   entries,
	}
	encoded, err := json.Marshal(index, jsontext.WithIndent("  "), json.Deterministic(true), json.FormatNilSliceAsNull(true))
	if err != nil {
		return fmt.Errorf("encode run index: %w", err)
	}
	return writeFile(filepath.Join(r.dir, "index.json"), append(encoded, '\n'))
}

func (r *reportRun) sampleSummary(entries []indexEntry) *sampleIndex {
	if r.sample == nil {
		return nil
	}
	recorded := make(map[string]bool, len(entries))
	for _, entry := range entries {
		recorded[entry.ID] = true
	}
	summary := *r.sample
	summary.NotExecuted = nil
	for _, id := range r.sample.IDs {
		if !recorded[id] {
			summary.NotExecuted = append(summary.NotExecuted, id)
		}
	}
	return &summary
}

// caseDefinition repeats the fixture values that define a case, so a report
// reproduces the run even after the fixture file changes.
type caseDefinition struct {
	Manifest string            `json:"manifest"`
	Args     []string          `json:"args"`
	Setup    []string          `json:"setup,omitempty"`
	Files    map[string]string `json:"files,omitempty"`
}

type invocation struct {
	Binary    string `json:"binary"`
	Directory string `json:"directory"`
	Timeout   string `json:"timeout"`
}

type runOutcome struct {
	Status     string `json:"status"`
	ExitCode   *int   `json:"exit_code,omitzero"`
	Detail     string `json:"detail,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// A skipped stage carries no output, workspace snapshot, or changes, because it
// never ran.
type stage struct {
	Name    string      `json:"name"`
	Argv    []string    `json:"argv"`
	Outcome runOutcome  `json:"outcome"`
	Stdout  *fileRecord `json:"stdout,omitzero"`
	Stderr  *fileRecord `json:"stderr,omitzero"`
	After   *snapshot   `json:"after,omitzero"`
	Changes *changeSet  `json:"changes,omitzero"`
}

type stageRun struct {
	record stage
	stdout []byte
	stderr []byte
}

type caseReport struct {
	Command    string         `json:"command"`
	ID         string         `json:"id"`
	Smoke      bool           `json:"smoke"`
	Fixture    fileRecord     `json:"fixture"`
	Definition caseDefinition `json:"definition"`
	Invocation invocation     `json:"invocation"`
	Before     snapshot       `json:"before"`
	Setup      *stage         `json:"setup,omitzero"`
	Main       stage          `json:"main"`
}

func persistCase(dir string, report caseReport, runs []stageRun) error {
	for _, run := range runs {
		if run.record.Stdout != nil {
			if err := writeFile(filepath.Join(dir, run.record.Stdout.Path), run.stdout); err != nil {
				return err
			}
		}
		if run.record.Stderr != nil {
			if err := writeFile(filepath.Join(dir, run.record.Stderr.Path), run.stderr); err != nil {
				return err
			}
		}
	}
	encoded, err := json.Marshal(report, jsontext.WithIndent("  "), json.Deterministic(true), json.FormatNilSliceAsNull(true))
	if err != nil {
		return fmt.Errorf("encode case report: %w", err)
	}
	return writeFile(filepath.Join(dir, "report.json"), append(encoded, '\n'))
}

// writeFile writes through a temporary name, so an interrupted run leaves no
// partial file.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary := path + ".partial"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// Symlinks record their target and are never followed.
type fileEntry struct {
	Path   string  `json:"path"`
	Kind   string  `json:"kind"`
	Mode   string  `json:"mode"`
	Size   int64   `json:"size"`
	SHA256 string  `json:"sha256,omitempty"`
	Target string  `json:"target,omitempty"`
	Text   *string `json:"text,omitzero"`
}

type snapshot []fileEntry

// Text bytes are kept for small UTF-8 files, so lucy.yaml and lucy-lock.yaml
// survive as written even after the workspace is removed.
func snapshotTree(root string) (snapshot, error) {
	entries := snapshot{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		record := fileEntry{
			Path: filepath.ToSlash(relative),
			Mode: fmt.Sprintf("%04o", info.Mode().Perm()),
		}
		switch mode := info.Mode(); {
		case mode.IsDir():
			record.Kind = "dir"
		case mode&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			record.Kind = "symlink"
			record.Target = target
		case mode.IsRegular():
			text, digest, err := readRegular(path, info.Size())
			if err != nil {
				return err
			}
			record.Kind = "file"
			record.Size = info.Size()
			record.SHA256 = digest
			record.Text = text
		default:
			record.Kind = "other"
		}
		entries = append(entries, record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b fileEntry) int {
		return strings.Compare(a.Path, b.Path)
	})
	return entries, nil
}

// Large files stream through the hash without being loaded.
func readRegular(path string, size int64) (*string, string, error) {
	if size <= inlineTextLimit {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", err
		}
		sum := sha256.Sum256(data)
		return textOf(data), hex.EncodeToString(sum[:]), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return nil, "", err
	}
	return nil, hex.EncodeToString(digest.Sum(nil)), nil
}

func textOf(data []byte) *string {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil
	}
	text := string(data)
	return &text
}

type changeSet struct {
	Created []string `json:"created"`
	Deleted []string `json:"deleted"`
	Changed []string `json:"changed"`
}

// diffSnapshots lists paths that appeared, disappeared, or differ in kind,
// mode, size, digest, or symlink target. Both snapshots are path-sorted, so
// each list is too.
func diffSnapshots(before, after snapshot) changeSet {
	previous := make(map[string]fileEntry, len(before))
	for _, entry := range before {
		previous[entry.Path] = entry
	}
	present := make(map[string]bool, len(after))
	changes := changeSet{
		Created: []string{},
		Deleted: []string{},
		Changed: []string{},
	}
	for _, entry := range after {
		present[entry.Path] = true
		old, ok := previous[entry.Path]
		switch {
		case !ok:
			changes.Created = append(changes.Created, entry.Path)
		case !sameEntry(old, entry):
			changes.Changed = append(changes.Changed, entry.Path)
		}
	}
	for _, entry := range before {
		if !present[entry.Path] {
			changes.Deleted = append(changes.Deleted, entry.Path)
		}
	}
	return changes
}

func sameEntry(a, b fileEntry) bool {
	return a.Kind == b.Kind &&
		a.Mode == b.Mode &&
		a.Size == b.Size &&
		a.SHA256 == b.SHA256 &&
		a.Target == b.Target
}

// Anything else is an error, because a fixture-built workspace cannot contain
// it.
func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("copy %s: unsupported entry type %s", path, entry.Type())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeFile(destination, data)
	})
}
