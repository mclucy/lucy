// Package testkit resolves the repository paths and prerequisites that the e2e
// suites run against.
//
// Each suite is a test binary under the e2e build tag, so this package is linked
// only there. The flags below replace the per-suite command-line flags that the
// suites needed while they were commands.
package testkit

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var (
	lucyBinary = flag.String("lucy", "", "path to the Lucy binary (default: dist/lucy under the repository root)")
	sandboxDir = flag.String("sandboxes", "", "directory holding generated sandboxes (default: .sandboxes under the repository root)")
)

var (
	rootOnce sync.Once
	rootPath string
	rootErr  error
)

// RepoRoot returns the repository root that contains the running suite.
func RepoRoot(t *testing.T) string {
	t.Helper()
	rootOnce.Do(func() { rootPath, rootErr = resolveRoot() })
	if rootErr != nil {
		t.Fatalf("resolve repository root: %v", rootErr)
	}
	return rootPath
}

// resolveRoot locates the repository from this source file. Tests run with the
// package directory as their working directory, so the source location is the
// only path that stays stable across suites.
func resolveRoot() (string, error) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("source location unavailable")
	}
	for dir := filepath.Dir(source); ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", source)
		}
		dir = parent
	}
}

// Lucy returns the absolute path to the Lucy binary that the suites invoke. It
// fails the test when the binary is absent so a suite never reports success
// without running the CLI it verifies.
func Lucy(t *testing.T) string {
	t.Helper()
	path := *lucyBinary
	if path == "" {
		path = filepath.Join(RepoRoot(t), "dist", "lucy")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve Lucy binary: %v", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		t.Fatalf("Lucy binary unavailable (%s): build it with `task build` first: %v", absolute, err)
	}
	if info.IsDir() {
		t.Fatalf("Lucy binary is a directory: %s", absolute)
	}
	return absolute
}

// Sandboxes returns the absolute directory that holds generated sandboxes.
func Sandboxes(t *testing.T) string {
	t.Helper()
	path := *sandboxDir
	if path == "" {
		path = filepath.Join(RepoRoot(t), ".sandboxes")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve sandbox directory: %v", err)
	}
	return absolute
}

// Sandbox returns the generated sandbox directory for id. It fails the test when
// the directory is absent, so a scenario cannot pass by finding nothing to probe.
func Sandbox(t *testing.T, id string) string {
	t.Helper()
	dir := filepath.Join(Sandboxes(t), id)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("sandbox %s is not generated (%s): run `task sandboxes:gen -- --only %s`", id, dir, id)
	}
	return dir
}

// HasSandbox reports whether the generated sandbox directory for id exists.
func HasSandbox(t *testing.T, id string) bool {
	t.Helper()
	info, err := os.Stat(filepath.Join(Sandboxes(t), id))
	return err == nil && info.IsDir()
}
