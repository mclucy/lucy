package probe

import (
	"archive/zip"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPaperDetectorIntegration_PaperFixtureProjectsToRuntimeInfo(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
		InvalidateServerInfo()
	})

	workDir := t.TempDir()
	paperJar := writeProbeFixtureJar(
		t,
		filepath.Join(probePaperFixtureRoot(t), "test_paper", "paper"),
		"paper.jar",
	)
	copyProbeFixture(t, paperJar, filepath.Join(workDir, "paper.jar"))

	observed := ServerInfoAt(workDir)
	if observed.Runtime == nil {
		t.Fatal("expected runtime info for paper fixture")
	}

	primary := observed.Runtime.PrimaryRuntimeIdentity()
	if primary == nil {
		t.Fatalf("expected primary runtime identity, got %+v", observed.Runtime)
	}
	if got := string(primary.Name); got != "paper" {
		t.Fatalf("expected paper runtime identity, got %q (%+v)", got, observed.Runtime.RuntimeIdentities)
	}
	if observed.Runtime.Topology == nil {
		t.Fatalf("expected runtime topology for paper fixture")
	}
	if got := observed.Runtime.Topology.PrimaryNode; got != RuntimeNodePaper {
		t.Fatalf("expected primary runtime node %q, got %q", RuntimeNodePaper, got)
	}
}

func TestPaperDetectorIntegration_ContradictoryEvidenceDoesNotProducePaperRuntime(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
		InvalidateServerInfo()
	})

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
	copyProbeFixture(t, contradictionJar, filepath.Join(workDir, "contradiction.jar"))

	observed := ServerInfoAt(workDir)
	if observed.Runtime == nil {
		t.Fatal("expected runtime info for contradiction fixture")
	}
	if observed.Runtime.Topology == nil {
		t.Fatalf("expected runtime topology for contradiction fixture")
	}

	if got := observed.Runtime.Topology.PrimaryNode; got == RuntimeNodePaper || got == RuntimeNodePaperFork {
		t.Fatalf("expected contradiction to avoid paper lineage, got primary node %q", got)
	}

	if primary := observed.Runtime.PrimaryRuntimeIdentity(); primary != nil {
		if got := string(primary.Name); got == "paper" || got == "paper-fork" {
			t.Fatalf("expected contradiction to avoid paper runtime identity, got %q", got)
		}
	}
}

func probePaperFixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("cannot locate test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "internal", "detector", "testdata", "paper_family"))
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
