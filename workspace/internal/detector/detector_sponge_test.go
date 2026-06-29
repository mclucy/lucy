package detector

import (
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestSpongeServerDetector_SpongeVanillaFixture(t *testing.T) {
	t.Parallel()

	jarPath := spongeFixtureJar(t, "test_sponge")
	reader := openZipForTest(t, jarPath)
	det := &spongeServerDetector{}

	evidence, err := det.Detect(jarPath, reader, nil)
	if err != nil {
		t.Fatalf("detect spongevanilla: %v", err)
	}
	if evidence == nil {
		t.Fatal("expected spongevanilla evidence, got nil")
	}
	if evidence.GameVersion != "1.21.10" {
		t.Fatalf("game version: got %q want 1.21.10", evidence.GameVersion)
	}
	if evidence.Topology == nil || evidence.Topology.PrimaryNode != types.RuntimeNodeSponge {
		t.Fatalf("topology: %+v", evidence.Topology)
	}
	if !evidence.Topology.HasCapability(types.CapabilitySpongePlugins) {
		t.Fatal("expected sponge_plugins capability")
	}
	if evidence.Topology.HasCapability(types.CapabilityForgeMods) {
		t.Fatal("vanilla sponge must not claim forge mods")
	}
}

func TestSpongeServerDetector_SpongeForgeFixture(t *testing.T) {
	t.Parallel()

	jarPath := spongeFixtureJar(t, "test_sponge_forge")
	reader := openZipForTest(t, jarPath)
	det := &spongeServerDetector{}

	evidence, err := det.Detect(jarPath, reader, nil)
	if err != nil {
		t.Fatalf("detect spongeforge: %v", err)
	}
	if evidence == nil {
		t.Fatal("expected spongeforge evidence, got nil")
	}
	if evidence.GameVersion != "1.21.10" {
		t.Fatalf("game version: got %q want 1.21.10", evidence.GameVersion)
	}
	if evidence.Topology == nil || evidence.Topology.PrimaryNode != types.RuntimeNodeSponge {
		t.Fatalf("topology: %+v", evidence.Topology)
	}
	if !evidence.Topology.HasCapability(types.CapabilitySpongePlugins) {
		t.Fatal("expected sponge_plugins capability")
	}
	if !evidence.Topology.HasCapability(types.CapabilityForgeMods) {
		t.Fatal("spongeforge must claim forge mods")
	}
}

func TestSpongeServerDetector_SpongeNeoFixture(t *testing.T) {
	t.Parallel()

	jarPath := spongeFixtureJar(t, "test_sponge_neoforge")
	reader := openZipForTest(t, jarPath)
	det := &spongeServerDetector{}

	evidence, err := det.Detect(jarPath, reader, nil)
	if err != nil {
		t.Fatalf("detect spongeneo: %v", err)
	}
	if evidence == nil {
		t.Fatal("expected spongeneo evidence, got nil")
	}
	if evidence.GameVersion != "1.21.10" {
		t.Fatalf("game version: got %q want 1.21.10", evidence.GameVersion)
	}
	if evidence.Topology == nil || evidence.Topology.PrimaryNode != types.RuntimeNodeSponge {
		t.Fatalf("topology: %+v", evidence.Topology)
	}
	if !evidence.Topology.HasCapability(types.CapabilityNeoforgeMods) {
		t.Fatal("spongeneo must claim neoforge mods")
	}
}

func TestSpongeServerDetector_UsesSpecificationTitleOverImplementationTitle(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"META-INF/MANIFEST.MF": "" +
			"Manifest-Version: 1.0\n" +
			"Specification-Title: SpongeVanilla\n" +
			"Specification-Vendor: SpongePowered\n" +
			"Implementation-Title: wrong-subproject-name\n" +
			"Implementation-Vendor: SpongePowered\n" +
			"Implementation-Version: 1.21.10-17.0.0\n",
	}
	jarPath := writeRootJar(t, "spongevanilla-spec-title.jar", files)
	reader := openZipForTest(t, jarPath)
	det := &spongeServerDetector{}

	evidence, err := det.Detect(jarPath, reader, nil)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if evidence == nil || evidence.GameVersion != "1.21.10" {
		t.Fatalf("expected spongevanilla from Specification-Title, got %+v", evidence)
	}
}

func TestSpongeServerDetector_RejectsNonSpongeJar(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nImplementation-Title: NotSponge\nImplementation-Vendor: SpongePowered\nImplementation-Version: 1.21.10-17.0.0\n",
	}
	jarPath := writeRootJar(t, "random-server.jar", files)
	reader := openZipForTest(t, jarPath)
	det := &spongeServerDetector{}

	evidence, err := det.Detect(jarPath, reader, nil)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if evidence != nil {
		t.Fatalf("expected nil for non-sponge jar name, got %+v", evidence)
	}
}

func spongeFixtureJar(t *testing.T, dirName string) string {
	t.Helper()

	root := filepath.Clean(filepath.Join("..", "..", "..", dirName))
	entries, err := filepath.Glob(filepath.Join(root, "sponge*.jar"))
	if err != nil {
		t.Fatalf("glob sponge fixtures in %s: %v", root, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one sponge*.jar in %s, got %v", root, entries)
	}
	return entries[0]
}
