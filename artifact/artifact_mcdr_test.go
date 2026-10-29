package artifact

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
)

func TestReadModulesMCDRReadsRequirementsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.mcdr")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	archive := zip.NewWriter(file)
	descriptor, err := archive.Create("mcdreforged.plugin.json")
	if err != nil {
		t.Fatalf("create descriptor: %v", err)
	}
	if _, err := descriptor.Write([]byte(
		`{"id":"demo","version":"1.0.0","requirements_file":"requirements.txt"}`,
	)); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}
	requirements, err := archive.Create("requirements.txt")
	if err != nil {
		t.Fatalf("create requirements: %v", err)
	}
	if _, err := requirements.Write([]byte(
		"# runtime dependencies\nrequests>=2.0\n\n-r other.txt\n",
	)); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	modules, err := ReadModules(context.Background(), path, catalog.EcosystemMCDR)
	if err != nil {
		t.Fatalf("ReadModules() error = %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("ReadModules() modules = %d, want 1", len(modules))
	}
	var python []string
	for _, dependency := range modules[0].Dependencies {
		if dependency.Kind == catalog.DependencyKindPython {
			python = append(python, dependency.ID)
		}
	}
	want := []string{"requests>=2.0"}
	if !slices.Equal(python, want) {
		t.Fatalf("python requirements = %v, want %v", python, want)
	}
}

func TestReadModulesMCDRDefaultsRequirementsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.mcdr")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	archive := zip.NewWriter(file)
	descriptor, err := archive.Create("mcdreforged.plugin.json")
	if err != nil {
		t.Fatalf("create descriptor: %v", err)
	}
	if _, err := descriptor.Write([]byte(`{"id":"demo","version":"1.0.0"}`)); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}
	requirements, err := archive.Create("requirements.txt")
	if err != nil {
		t.Fatalf("create requirements: %v", err)
	}
	if _, err := requirements.Write([]byte("watchdog>=5\n")); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	modules, err := ReadModules(context.Background(), path, catalog.EcosystemMCDR)
	if err != nil {
		t.Fatalf("ReadModules() error = %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("ReadModules() modules = %d, want 1", len(modules))
	}
	var python []string
	for _, dependency := range modules[0].Dependencies {
		if dependency.Kind == catalog.DependencyKindPython {
			python = append(python, dependency.ID)
		}
	}
	want := []string{"watchdog>=5"}
	if !slices.Equal(python, want) {
		t.Fatalf("python requirements = %v, want %v", python, want)
	}
}
