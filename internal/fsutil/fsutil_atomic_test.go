//go:build integration

package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicWritePreservesExistingPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not describe Windows file access")
	}
	for _, test := range []struct {
		name string
		mode os.FileMode
	}{
		{"shared", 0o644},
		{"group-readable", 0o640},
		{"private", 0o600},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "state.yaml")
			if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, test.mode); err != nil {
				t.Fatal(err)
			}
			if err := AtomicWrite(path, []byte("after")); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != test.mode {
				t.Errorf("replacement permissions = %04o, want %04o", info.Mode().Perm(), test.mode)
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(contents) != "after" {
				t.Errorf("replacement contains %q, want updated state", contents)
			}
		})
	}
}
