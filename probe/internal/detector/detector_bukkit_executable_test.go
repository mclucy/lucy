package detector

import (
	"os"
	"path/filepath"
	"strings"
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

func TestCraftBukkitFamilyDetector_FamilyMissStillRunsBrandRules(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(dir, "META-INF", "MANIFEST.MF"),
		[]byte("Manifest-Version: 1.0\nMain-Class: org.bukkit.craftbukkit.Main\n\n"),
	)
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(dir, "META-INF", "libraries.list"),
		[]byte("org.purpurmc.purpur:purpur-api:1.21.11-R0.1-SNAPSHOT\n"),
	)

	judgment := newPaperJudgment()
	judgment.bukkitConfirmed = true
	judgment.observations = paperObservations{
		librariesListEntries: []string{"org.purpurmc.purpur:purpur-api:1.21.11-R0.1-SNAPSHOT"},
	}
	reasonPaperFamily(&judgment)
	if judgment.familyResult != familyMiss {
		t.Fatalf("expected family miss before brand rules, got %v", judgment.familyResult)
	}

	evidence, err := (&craftBukkitFamilyDetector{}).Detect(dir, nil, nil)
	if err != nil {
		t.Fatalf("detect bukkit candidate with family miss brand recovery: %v", err)
	}
	if evidence == nil {
		t.Fatalf("expected non-terminal family miss to continue to brand rules")
	}
	if len(evidence.RuntimeIdentities) == 0 || evidence.RuntimeIdentities[0].Name != "purpur" {
		t.Fatalf("expected brand recovery to project purpur identity, got %+v", evidence.RuntimeIdentities)
	}
	if evidence.TopologySeed == nil || len(evidence.TopologySeed.Nodes) == 0 || evidence.TopologySeed.Nodes[0].ID != bukkitNodePaperFork {
		t.Fatalf("expected brand recovery to project paper-fork topology, got %+v", evidence.TopologySeed)
	}
}

func TestCraftBukkitFamilyDetector_ContradictoryEvidenceFailsClosed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(dir, "META-INF", "MANIFEST.MF"),
		[]byte("Manifest-Version: 1.0\nMain-Class: org.bukkit.craftbukkit.Main\n\n"),
	)
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(dir, "META-INF", "libraries.list"),
		[]byte("io.papermc.paper:paper-api:1.21.11-R0.1-SNAPSHOT\norg.purpurmc.purpur:purpur-api:1.21.11-R0.1-SNAPSHOT\n"),
	)

	judgment := newPaperJudgment()
	judgment.bukkitConfirmed = true
	judgment.observations = paperObservations{
		librariesListEntries: []string{
			"io.papermc.paper:paper-api:1.21.11-R0.1-SNAPSHOT",
			"org.purpurmc.purpur:purpur-api:1.21.11-R0.1-SNAPSHOT",
		},
	}
	reasonPaperFamily(&judgment)
	attributePaperBrand(&judgment)
	resolvePaperContradictions(&judgment)
	if judgment.familyResult != familyContradiction {
		t.Fatalf("expected contradictory brands to set family contradiction, got %v", judgment.familyResult)
	}
	if judgment.contradictionState == "" {
		t.Fatalf("expected descriptive contradiction state")
	}
	if !reasonsContainSubstring(judgment.reasons, judgment.contradictionState) {
		t.Fatalf("expected reasons to record contradiction state, got %#v", judgment.reasons)
	}

	evidence, err := (&craftBukkitFamilyDetector{}).Detect(dir, nil, nil)
	if err != nil {
		t.Fatalf("detect contradictory paper candidate: %v", err)
	}
	if evidence == nil {
		return
	}
	if len(evidence.RuntimeIdentities) == 0 {
		t.Fatalf("expected at least one runtime identity when evidence is returned")
	}
	if evidence.RuntimeIdentities[0].Name == "paper" || evidence.RuntimeIdentities[0].Name == "purpur" {
		t.Fatalf("expected contradictory evidence to fail closed, got %+v", evidence.RuntimeIdentities)
	}
	if evidence.TopologySeed != nil && len(evidence.TopologySeed.Nodes) > 0 {
		primary := evidence.TopologySeed.Nodes[0].ID
		if primary == bukkitNodePaper || primary == bukkitNodePaperFork {
			t.Fatalf("expected contradictory evidence to avoid paper promotion, got %+v", evidence.TopologySeed.Nodes)
		}
	}
}

func reasonsContainSubstring(reasons []string, want string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, want) {
			return true
		}
	}
	return false
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
