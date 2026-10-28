package serverregistry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	serverpkg "github.com/mclucy/lucy/server"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	probe := t.TempDir()
	target := filepath.Join(probe, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("create symlink probe target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(probe, "link")); err != nil {
		t.Skipf("platform cannot create the directory links the registry requires: %v", err)
	}
	return &Registry{directory: t.TempDir()}
}

func writeManifest(t *testing.T, root, name string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create workspace %s: %v", root, err)
	}
	manifest := &document.Manifest{
		SchemaVersion: document.ManifestSchemaVersion,
		Name:          name,
		Server: document.ManifestServer{
			Minecraft: "1.21.4",
			Core: document.ManifestCore{
				Product:        serverpkg.ProductPaper,
				ProductVersion: "1.21.4",
			},
		},
	}
	if err := document.ManifestFile(root).Write(manifest); err != nil {
		t.Fatalf("write manifest in %s: %v", root, err)
	}
}

func register(t *testing.T, registry *Registry, root string) []Entry {
	t.Helper()
	writer, err := workspacewrite.Lock(root, nil)
	if err != nil {
		t.Fatalf("lock workspace %s: %v", root, err)
	}
	duplicates, registerErr := registry.Register(root)
	closeErr := writer.Close()
	if registerErr != nil {
		t.Fatalf("Register(%s): %v", root, registerErr)
	}
	if closeErr != nil {
		t.Fatalf("close workspace lock %s: %v", root, closeErr)
	}
	return duplicates
}

func unregister(t *testing.T, registry *Registry, root string) {
	t.Helper()
	writer, err := workspacewrite.Lock(root, nil)
	if err != nil {
		t.Fatalf("lock workspace %s: %v", root, err)
	}
	unregisterErr := registry.Unregister(root)
	closeErr := writer.Close()
	if unregisterErr != nil {
		t.Fatalf("Unregister(%s): %v", root, unregisterErr)
	}
	if closeErr != nil {
		t.Fatalf("close workspace lock %s: %v", root, closeErr)
	}
}

func resolvedPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return resolved
}

func entryAt(t *testing.T, entries []Entry, path string) Entry {
	t.Helper()
	for _, entry := range entries {
		if entry.Path == path {
			return entry
		}
	}
	t.Fatalf("no entry for path %s in %v", path, entries)
	return Entry{}
}

func TestRegisterCanonicalPathIsIdempotent(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	registry := newTestRegistry(t)
	root := filepath.Join(base, "survival")
	writeManifest(t, root, "  survival  ")
	wantPath := resolvedPath(t, root)

	if duplicates := register(t, registry, root); len(duplicates) != 0 {
		t.Fatalf("first Register duplicates = %v, want none", duplicates)
	}
	register(t, registry, root)
	entries, err := registry.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != wantPath || entries[0].Name != "survival" {
		t.Fatalf("List after re-registering = %v, want one entry at %s named survival", entries, wantPath)
	}

	t.Run("relative path", func(t *testing.T) {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("get working directory: %v", err)
		}
		relative, err := filepath.Rel(cwd, root)
		if err != nil {
			t.Skipf("temp directory is not on the same volume as the test working directory: %v", err)
		}
		register(t, registry, relative)
		entries, err := registry.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(entries) != 1 || entries[0].Path != wantPath || entries[0].Name != "survival" {
			t.Fatalf("List after relative Register = %v, want one entry at %s named survival", entries, wantPath)
		}
	})

	t.Run("symlink alias", func(t *testing.T) {
		alias := filepath.Join(base, "survival-alias")
		if err := os.Symlink(root, alias); err != nil {
			t.Skipf("create symlink alias: %v", err)
		}
		register(t, registry, alias)
		entries, err := registry.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(entries) != 1 || entries[0].Path != wantPath || entries[0].State != StateAvailable {
			t.Fatalf("List after alias Register = %v, want one available entry at %s", entries, wantPath)
		}
	})
}

func TestDuplicateNamesRemainDistinctPaths(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	registry := newTestRegistry(t)
	firstRoot := filepath.Join(base, "first")
	secondRoot := filepath.Join(base, "second")
	writeManifest(t, firstRoot, "shared")
	writeManifest(t, secondRoot, "shared")
	firstPath := resolvedPath(t, firstRoot)
	secondPath := resolvedPath(t, secondRoot)

	register(t, registry, firstRoot)
	duplicates := register(t, registry, secondRoot)
	if len(duplicates) != 1 || duplicates[0].Path != firstPath {
		t.Fatalf("Register duplicate = %v, want only %s", duplicates, firstPath)
	}

	entries, err := registry.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List = %v, want both same-named paths", entries)
	}
	for _, path := range []string{firstPath, secondPath} {
		if entry := entryAt(t, entries, path); entry.Name != "shared" || entry.State != StateAvailable {
			t.Fatalf("entry %s = %+v, want available name shared", path, entry)
		}
	}

	if _, err := registry.Resolve("shared"); err == nil {
		t.Fatal("Resolve(shared) error = nil, want ambiguity error")
	} else {
		for _, path := range []string{firstPath, secondPath} {
			if !strings.Contains(err.Error(), path) {
				t.Fatalf("ambiguity error %q does not identify %s", err, path)
			}
		}
	}

	unregister(t, registry, firstRoot)
	entry, err := registry.Resolve("shared")
	if err != nil {
		t.Fatalf("Resolve(shared) after removing the first path: %v", err)
	}
	if entry.Path != secondPath {
		t.Fatalf("Resolve(shared) = %s, want surviving path %s", entry.Path, secondPath)
	}

	unregister(t, registry, secondRoot)
	entries, err = registry.List()
	if err != nil {
		t.Fatalf("List after removing both paths: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("List after removing both paths = %v, want empty", entries)
	}
	if _, err := registry.Resolve("shared"); err == nil {
		t.Fatal("Resolve(shared) after unregistering both paths error = nil, want not-found error")
	}
}

func TestListKeepsMissingTargetsAndReloadsNames(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	registry := newTestRegistry(t)
	missingRoot := filepath.Join(base, "missing")
	renamedRoot := filepath.Join(base, "renamed")
	writeManifest(t, missingRoot, "ghost")
	writeManifest(t, renamedRoot, "before")
	missingPath := resolvedPath(t, missingRoot)
	renamedPath := resolvedPath(t, renamedRoot)

	register(t, registry, missingRoot)
	register(t, registry, renamedRoot)
	if err := os.RemoveAll(missingRoot); err != nil {
		t.Fatalf("remove registered target: %v", err)
	}

	for range 2 {
		entries, err := registry.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("List = %v, want the missing target retained", entries)
		}
		missing := entryAt(t, entries, missingPath)
		if missing.State != StateMissing || missing.Problem == "" {
			t.Fatalf("missing entry = %+v, want a diagnosed missing state", missing)
		}
	}

	writeManifest(t, renamedRoot, "after")
	entries, err := registry.List()
	if err != nil {
		t.Fatalf("List after manifest rename: %v", err)
	}
	if entry := entryAt(t, entries, renamedPath); entry.Name != "after" {
		t.Fatalf("entry after rename = %+v, want fresh name after", entry)
	}
	if _, err := registry.Resolve("before"); err == nil {
		t.Fatal("Resolve(before) after manifest rename error = nil, want not-found error")
	}
	entry, err := registry.Resolve("after")
	if err != nil {
		t.Fatalf("Resolve(after): %v", err)
	}
	if entry.Path != renamedPath {
		t.Fatalf("Resolve(after) = %s, want %s", entry.Path, renamedPath)
	}
}
