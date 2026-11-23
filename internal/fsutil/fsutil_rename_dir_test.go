package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenameDirRootMovesDirectoryWithContents(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".lucy", "displaced"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lucy", "displaced", "kept.txt"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })

	if err := RenameDirRoot(root, filepath.Join(".lucy", "displaced"), "kept"); err != nil {
		t.Fatalf("RenameDirRoot() error = %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "kept", "kept.txt")); err != nil || string(got) != "kept" {
		t.Fatalf("renamed content = %q, %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".lucy", "displaced")); !os.IsNotExist(err) {
		t.Fatalf("source still present: %v", err)
	}
	if err := RenameDirRoot(root, "kept", "../outside"); err == nil {
		t.Fatal("RenameDirRoot() accepted a destination outside the root")
	}
}
