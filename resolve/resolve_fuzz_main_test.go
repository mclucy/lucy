package resolve_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain blocks the process-wide network cache before any resolve test can
// initialize it. Resolution tests drive the catalogue through a stub HTTP
// client, and a live cache would otherwise replay a response generated for a
// different fuzz input under the same project URL.
func TestMain(m *testing.M) {
	os.Exit(runResolveTests(m))
}

func runResolveTests(m *testing.M) (code int) {
	root, err := os.MkdirTemp("", "lucy-resolve-tests-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepare resolve test root: %v\n", err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			fmt.Fprintf(os.Stderr, "remove resolve test root: %v\n", err)
		}
	}()
	home := filepath.Join(root, "home")
	cacheParents := []string{
		filepath.Join(home, "Library", "Caches"),
		filepath.Join(root, "xdg-cache"),
		filepath.Join(root, "local-app-data"),
	}
	for _, parent := range cacheParents {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "prepare resolve test cache parent: %v\n", err)
			return 1
		}
		// A regular file where the lucy cache expects its root makes the
		// network cache initialize disabled and fall back to the stub
		// transport on every lookup. Other user-cache consumers still see a
		// usable directory.
		if err := os.WriteFile(filepath.Join(parent, "lucy"), []byte("blocked by resolve tests"), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "block resolve test cache: %v\n", err)
			return 1
		}
	}
	keys := []string{"HOME", "XDG_CACHE_HOME", "LOCALAPPDATA"}
	previous := make(map[string]string, len(keys))
	for _, key := range keys {
		previous[key] = os.Getenv(key)
	}
	set := map[string]string{
		"HOME":           home,
		"XDG_CACHE_HOME": filepath.Join(root, "xdg-cache"),
		"LOCALAPPDATA":   filepath.Join(root, "local-app-data"),
	}
	for key, value := range set {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "set %s for resolve tests: %v\n", key, err)
			return 1
		}
	}
	defer func() {
		for key, value := range previous {
			_ = os.Setenv(key, value)
		}
	}()
	return m.Run()
}
