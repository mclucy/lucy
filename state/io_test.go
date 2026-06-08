package state

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAtomicWriteReplacesFileAndLeavesNoTempFiles(t *testing.T) {
	workDir := t.TempDir()
	target := filepath.Join(workDir, "state.yaml")

	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	if err := AtomicWrite(target, []byte("new state"), 0o640); err != nil {
		t.Fatalf("AtomicWrite failed: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(data, []byte("new state")) {
		t.Fatalf("target content mismatch: got %q", data)
	}

	entries, err := os.ReadDir(workDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.yaml" {
		t.Fatalf("unexpected leftover temp files: %#v", entries)
	}
}

func TestSafeReadMissingFileIsNotError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	data, ok, err := SafeRead(path)
	if err != nil {
		t.Fatalf("SafeRead returned unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("SafeRead should report missing file")
	}
	if data != nil {
		t.Fatalf("SafeRead should return nil data for missing file")
	}
}

func TestReadWriteConfigRoundTrip(t *testing.T) {
	workDir := t.TempDir()
	config := ConfigDefaults()
	config.Sources.Preferred = "curseforge"

	if err := WriteConfig(workDir, &config); err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}

	loaded, ok, err := ReadConfig(workDir)
	if err != nil {
		t.Fatalf("ReadConfig failed: %v", err)
	}
	if !ok {
		t.Fatalf("ReadConfig should report file exists")
	}
	if loaded == nil {
		t.Fatalf("ReadConfig returned nil config")
	}
	if !reflect.DeepEqual(*loaded, config) {
		t.Fatalf("config mismatch after round-trip\nwant: %#v\ngot: %#v", config, *loaded)
	}
}

func TestAtomicWriteFailureLeavesOriginalTargetUntouched(t *testing.T) {
	workDir := t.TempDir()
	target := filepath.Join(workDir, "protected")

	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("create protected target dir: %v", err)
	}
	marker := filepath.Join(target, "keep.txt")
	if err := os.WriteFile(marker, []byte("original"), 0o600); err != nil {
		t.Fatalf("seed marker: %v", err)
	}

	err := AtomicWrite(target, []byte("replacement"), 0o600)
	if err == nil {
		t.Fatalf("expected AtomicWrite to fail when renaming over directory")
	}

	data, readErr := os.ReadFile(marker)
	if readErr != nil {
		t.Fatalf("read marker: %v", readErr)
	}
	if !bytes.Equal(data, []byte("original")) {
		t.Fatalf("existing target content changed after failure: %q", data)
	}

	entries, dirErr := os.ReadDir(workDir)
	if dirErr != nil {
		t.Fatalf("read dir: %v", dirErr)
	}
	if len(entries) != 1 || entries[0].Name() != "protected" {
		t.Fatalf("unexpected leftover temp files after failure: %#v", entries)
	}
}

func TestReadWriteManifestPreservesCompatiblePlatforms(t *testing.T) {
	workDir := t.TempDir()
	manifest := ManifestDefaults()
	manifest.Environment.GameVersion = "1.21.1"
	manifest.Environment.ModdingPlatform = "neoforge"
	manifest.Environment.CompatiblePlatforms = []string{"fabric", "mcdr", "sinytra"}
	manifest.Environment.ModdingPlatformVersion = "21.1.0"
	manifest.Packages = []ManifestPackage{{
		ID:      "neoforge/connector",
		Version: "compatible",
		Source:  "modrinth",
		Role:    RoleRequired,
		Side:    SideServer,
	}}

	if err := WriteManifest(workDir, &manifest); err != nil {
		t.Fatalf("WriteManifest failed: %v", err)
	}

	loaded, ok, err := ReadManifest(workDir)
	if err != nil {
		t.Fatalf("ReadManifest failed: %v", err)
	}
	if !ok || loaded == nil {
		t.Fatalf("expected manifest file to exist after write")
	}
	if !reflect.DeepEqual(*loaded, manifest) {
		t.Fatalf("manifest mismatch after round-trip\nwant: %#v\ngot: %#v", manifest, *loaded)
	}
}
