package detector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCraftBukkitFamilyDetector_RequiresBukkitConfirmation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(dir, "META-INF", "MANIFEST.MF"),
		[]byte("Manifest-Version: 1.0\nMain-Class: com.example.SomeOtherServer\n\n"),
	)

	evidence, err := (&craftBukkitFamilyDetector{}).Detect(dir, nil, nil)
	if err != nil {
		t.Fatalf("detect craftbukkit family without bukkit confirmation: %v", err)
	}
	if evidence != nil {
		t.Fatalf("expected nil evidence without bukkit confirmation, got %+v", evidence)
	}
}

func TestCraftBukkitFamilyDetector_SkipsFastPathBeforeBukkitConfirmation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(dir, "META-INF", "MANIFEST.MF"),
		[]byte("Manifest-Version: 1.0\nMain-Class: com.example.SomeOtherServer\nImplementation-Title: ExampleServer\n\n"),
	)
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(dir, "META-INF", "libraries.list"),
		[]byte("io.papermc.paper:paper-api:1.21.11-R0.1-SNAPSHOT\n"),
	)
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(dir, "io", "papermc", "paper", "Fake.class"),
		[]byte("paper-class-marker"),
	)

	// The detector intentionally has no hash fast-path hook before Stage 1.
	// Without Bukkit confirmation, Detect must return nil before any Paper-family
	// classification or future fast-path optimization could run.
	evidence, err := (&craftBukkitFamilyDetector{}).Detect(dir, nil, nil)
	if err != nil {
		t.Fatalf("detect paper-like non-bukkit candidate: %v", err)
	}
	if evidence != nil {
		t.Fatalf("expected nil evidence before fast-path ordering boundary, got %+v", evidence)
	}
}

func writeBukkitExecutableFixtureFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture dir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}
