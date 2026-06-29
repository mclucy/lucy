package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestSpongeDetectorIntegration_VanillaFixture(t *testing.T) {
	runSpongeIntegrationProbe(
		t,
		"test_sponge",
		types.CapabilitySpongePlugins,
		false,
		false,
	)
}

func TestSpongeDetectorIntegration_ForgeFixture(t *testing.T) {
	runSpongeIntegrationProbe(
		t,
		"test_sponge_forge",
		types.CapabilitySpongePlugins,
		true,
		false,
	)
}

func TestSpongeDetectorIntegration_NeoFixture(t *testing.T) {
	runSpongeIntegrationProbe(
		t,
		"test_sponge_neoforge",
		types.CapabilitySpongePlugins,
		false,
		true,
	)
}

func runSpongeIntegrationProbe(
	t *testing.T,
	fixtureDir string,
	wantSponge types.RuntimeCapability,
	wantForge bool,
	wantNeo bool,
) {
	t.Helper()

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(
		func() {
			_ = os.Chdir(originalWD)
			Invalidate()
		},
	)

	jarPath := spongeIntegrationFixtureJar(t, fixtureDir)
	workDir := t.TempDir()
	copyProbeFixture(t, jarPath, filepath.Join(workDir, filepath.Base(jarPath)))

	observed := NewAt(workDir)
	if observed.Runtime == nil {
		t.Fatal("expected runtime info for sponge fixture")
	}
	if observed.Topology == nil || !observed.Topology.Resolved() {
		t.Fatalf("expected resolved topology, got %+v", observed.Topology)
	}
	if got := observed.Topology.PrimaryNode; got != types.RuntimeNodeSponge {
		t.Fatalf("primary node: got %q want %q", got, types.RuntimeNodeSponge)
	}
	if !observed.Topology.HasCapability(wantSponge) {
		t.Fatal("missing sponge_plugins capability")
	}
	if wantForge != observed.Topology.HasCapability(types.CapabilityForgeMods) {
		t.Fatalf("forge mods capability: want %v", wantForge)
	}
	if wantNeo != observed.Topology.HasCapability(types.CapabilityNeoforgeMods) {
		t.Fatalf("neoforge mods capability: want %v", wantNeo)
	}
	if observed.Runtime.GameVersion != "1.21.10" {
		t.Fatalf("game version: got %q", observed.Runtime.GameVersion)
	}
}

func spongeIntegrationFixtureJar(t *testing.T, dirName string) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", dirName))
	entries, err := filepath.Glob(filepath.Join(root, "sponge*.jar"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one sponge jar in %s, got %v", root, entries)
	}
	return entries[0]
}
