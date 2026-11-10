package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportTargetUsesEmptyInvocationDirectory(t *testing.T) {
	dir := t.TempDir()
	inv := &Invocation{Directory: dir}
	got, err := importTarget(inv, "", "/sources/server.zip")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(dir) {
		t.Fatalf("target = %q, want %q", got, dir)
	}
}

func TestImportTargetDerivesSubdirectoryFromNonEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Invocation{Directory: dir}
	got, err := importTarget(inv, "", "/sources/All The Mods.mrpack")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "all-the-mods")
	if got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

func TestImportTargetResolvesExplicitDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Invocation{Directory: dir}
	got, err := importTarget(inv, "server", "/sources/server.zip")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "server"); got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

func TestImportSlugStripsArchiveSuffixes(t *testing.T) {
	tests := map[string]string{
		"/sources/Server.TAR.GZ": "server",
		"/sources/pack.zip":      "pack",
		"/sources/pack.mrpack":   "pack",
		"https://example.com/packs/All%20The%20Mods%209-1.21.mrpack": "all-the-mods-9-1-21",
	}
	for source, want := range tests {
		got, err := importSlug(source)
		if err != nil {
			t.Fatalf("importSlug(%q): %v", source, err)
		}
		if got != want {
			t.Fatalf("importSlug(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestImportSlugRejectsSourceWithoutName(t *testing.T) {
	if _, err := importSlug("https://example.com/"); err == nil {
		t.Fatal("importSlug returned no error for a source without a name")
	}
}

func TestImportURLDetection(t *testing.T) {
	tests := map[string]bool{
		"https://example.com/pack.mrpack": true,
		"http://example.com/server.zip":   true,
		"server.zip":                      false,
		"/abs/server.zip":                 false,
		"ftp://example.com/server.zip":    false,
		"file:///abs/server.zip":          false,
	}
	for source, want := range tests {
		if _, got := importURL(source); got != want {
			t.Fatalf("importURL(%q) = %t, want %t", source, got, want)
		}
	}
}

func TestValidImportSHA256(t *testing.T) {
	valid := strings.Repeat("a", 64)
	if !validImportSHA256(valid) {
		t.Fatal("64 hexadecimal characters rejected")
	}
	if validImportSHA256(strings.Repeat("a", 63)) {
		t.Fatal("63 characters accepted")
	}
	if validImportSHA256(strings.Repeat("g", 64)) {
		t.Fatal("non-hexadecimal characters accepted")
	}
}
