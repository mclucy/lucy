package workspace

import (
	"archive/zip"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mclucy/lucy/workspace/discovery"
)

func TestPaperDiscoveryIntegration_PaperFixtureDetectsRuntime(t *testing.T) {
	workDir := t.TempDir()
	paperJar := writeProbeFixtureJar(
		t,
		filepath.Join(probePaperFixtureRoot(t), "test_paper", "paper"),
		"paper.jar",
	)
	copyProbeFixture(t, paperJar, filepath.Join(workDir, "paper.jar"))

	observed := Observe(workDir, discovery.Request{})
	server := observed.Runtime()
	if server == nil {
		t.Fatal("expected a selected runtime for paper fixture")
	}

	primary := server.Installation.Identity.Primary
	if got := primary.Name.String(); got != "paper" {
		t.Fatalf(
			"expected Paper primary core, got %q (%+v)",
			got,
			server.Installation.Identity.Components,
		)
	}
}

func TestPaperDiscoveryIntegration_ContradictoryEvidenceDoesNotProducePaperRuntime(t *testing.T) {
	fixtureDir := t.TempDir()
	writeProbeFixtureFile(
		t,
		filepath.Join(fixtureDir, "META-INF", "MANIFEST.MF"),
		[]byte("Manifest-Version: 1.0\nMain-Class: org.bukkit.craftbukkit.Main\n\n"),
	)
	writeProbeFixtureFile(
		t,
		filepath.Join(fixtureDir, "META-INF", "libraries.list"),
		[]byte("io.papermc.paper:paper-api:1.21.11-R0.1-SNAPSHOT\norg.purpurmc.purpur:purpur-api:1.21.11-R0.1-SNAPSHOT\n"),
	)

	workDir := t.TempDir()
	contradictionJar := writeProbeFixtureJar(t, fixtureDir, "contradiction.jar")
	copyProbeFixture(
		t,
		contradictionJar,
		filepath.Join(workDir, "contradiction.jar"),
	)

	observed := Observe(workDir, discovery.Request{})

	// The paper-family recognizer must decline the jar rather than guess a
	// lineage; whatever identity survives must not be a paper fork.
	if server := observed.Runtime(); server != nil {
		got := server.Installation.Identity.Primary.Name.String()
		if got == "paper" || got == "paper-fork" {
			t.Fatalf(
				"expected contradiction to avoid paper runtime identity, got %q",
				got,
			)
		}
	}
}

func probePaperFixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("cannot locate test file path")
	}
	return filepath.Clean(
		filepath.Join(
			filepath.Dir(file),
			"discovery",
			"testdata",
			"paper_family",
		),
	)
}

func writeProbeFixtureJar(t *testing.T, sourceDir string, name string) string {
	t.Helper()

	jarPath := filepath.Join(t.TempDir(), name)
	file, err := os.Create(jarPath)
	if err != nil {
		t.Fatalf("create fixture jar: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	err = filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		entry, err := writer.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := entry.Write(data); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixture dir %s: %v", sourceDir, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close fixture jar writer: %v", err)
	}

	return jarPath
}

func writeProbeFixtureFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture dirs for %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture file %s: %v", path, err)
	}
}

func copyProbeFixture(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", dst, err)
	}
}
