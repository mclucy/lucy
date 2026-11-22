package fileidentity

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A caller may hand over a stat that predates a permission change. The
// observation must describe the file it read, because the identity it
// persists comes from the same read.
func TestObserveRootInfoReportsModeOfReadHandle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not modeled on Windows")
	}
	dir := t.TempDir()
	files, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale, err := files.Lstat("run.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "run.sh"), 0o644); err != nil {
		t.Fatal(err)
	}

	observation, err := NewMemo().ObserveRootInfo(files, "run.sh", stale, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := observation.Mode.Perm(); got != 0o644 {
		t.Fatalf("Mode.Perm() = %#o, want 0644", got)
	}
}
