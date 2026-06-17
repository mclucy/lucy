package detector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestCraftBukkitFamilyDetector_PaperFixtureClassifiesAsPaper(t *testing.T) {
	t.Parallel()

	fixtureRoot := paperFamilyFixtureRoot(t)
	paperDir := filepath.Join(fixtureRoot, "test_paper", "paper")

	evidence, err := (&craftBukkitFamilyDetector{}).Detect(paperDir, nil, nil)
	if err != nil {
		t.Fatalf("detect paper fixture: %v", err)
	}
	if evidence == nil {
		t.Fatalf("expected paper fixture evidence")
	}
	if len(evidence.RuntimeIdentities) == 0 {
		t.Fatalf("expected runtime identities for paper fixture")
	}
	if string(evidence.RuntimeIdentities[0].Name) != "paper" {
		t.Fatalf("expected primary runtime identity paper, got %+v", evidence.RuntimeIdentities)
	}
	if evidence.TopologySeed == nil {
		t.Fatalf("expected topology seed for paper fixture")
	}
	if evidence.TopologySeed.PrimaryNode != bukkitNodePaper {
		t.Fatalf("expected primary topology node %q, got %+v", bukkitNodePaper, evidence.TopologySeed)
	}
}

func TestCraftBukkitFamilyDetector_RuntimeProjection(t *testing.T) {
	t.Parallel()

	fixtureRoot := paperFamilyFixtureRoot(t)
	contradictionDir := t.TempDir()
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(contradictionDir, "META-INF", "MANIFEST.MF"),
		[]byte("Manifest-Version: 1.0\nMain-Class: org.bukkit.craftbukkit.Main\n\n"),
	)
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(contradictionDir, "META-INF", "libraries.list"),
		[]byte("io.papermc.paper:paper-api:1.21.11-R0.1-SNAPSHOT\norg.purpurmc.purpur:purpur-api:1.21.11-R0.1-SNAPSHOT\n"),
	)

	tests := []struct {
		name         string
		path         string
		wantIdentity string
		wantNode     types.RuntimeNodeID
	}{
		{
			name:         "paper fixture projects official paper runtime",
			path:         filepath.Join(fixtureRoot, "test_paper", "paper"),
			wantIdentity: "paper",
			wantNode:     bukkitNodePaper,
		},
		{
			name:         "paper fork fixture projects paper fork runtime",
			path:         filepath.Join(fixtureRoot, "test_folia", "folia"),
			wantIdentity: "folia",
			wantNode:     bukkitNodePaperFork,
		},
		{
			name:         "contradiction fails closed to bukkit runtime",
			path:         contradictionDir,
			wantIdentity: "bukkit",
			wantNode:     bukkitNodeBukkit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			evidence, err := (&craftBukkitFamilyDetector{}).Detect(tt.path, nil, nil)
			if err != nil {
				t.Fatalf("detect %s: %v", tt.name, err)
			}
			if evidence == nil {
				t.Fatalf("expected evidence for %s", tt.name)
			}
			if len(evidence.RuntimeIdentities) == 0 {
				t.Fatalf("expected runtime identities for %s", tt.name)
			}
			if string(evidence.RuntimeIdentities[0].Name) != tt.wantIdentity {
				t.Fatalf("expected primary runtime identity %q, got %+v", tt.wantIdentity, evidence.RuntimeIdentities)
			}
			if evidence.TopologySeed == nil {
				t.Fatalf("expected topology seed for %s", tt.name)
			}
			if evidence.TopologySeed.PrimaryNode != tt.wantNode {
				t.Fatalf("expected primary topology node %q, got %+v", tt.wantNode, evidence.TopologySeed)
			}
		})
	}
}

func TestCraftBukkitFamilyDetector_KnownPaperForkBrands(t *testing.T) {
	t.Parallel()

	fixtureRoot := paperFamilyFixtureRoot(t)
	tests := []struct {
		name  string
		brand string
	}{
		{name: "folia", brand: "folia"},
		{name: "divine", brand: "divine"},
		{name: "purpur", brand: "purpur"},
		{name: "leaf", brand: "leaf"},
		{name: "leaves", brand: "leaves"},
		{name: "reaper", brand: "reaper"},
		{name: "youer", brand: "youer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			brandDir := filepath.Join(fixtureRoot, "test_"+tt.brand, tt.brand)
			evidence, err := (&craftBukkitFamilyDetector{}).Detect(brandDir, nil, nil)
			if err != nil {
				t.Fatalf("detect %s fixture: %v", tt.brand, err)
			}
			if evidence == nil {
				t.Fatalf("expected evidence for %s fixture", tt.brand)
			}
			if evidence.TopologySeed == nil {
				t.Fatalf("expected topology seed for %s fixture", tt.brand)
			}
			if evidence.TopologySeed.PrimaryNode != bukkitNodePaperFork {
				t.Fatalf("expected primary topology node %q for %s, got %+v", bukkitNodePaperFork, tt.brand, evidence.TopologySeed)
			}
			if len(evidence.RuntimeIdentities) == 0 {
				t.Fatalf("expected runtime identities for %s fixture", tt.brand)
			}
			if string(evidence.RuntimeIdentities[0].Name) != tt.brand {
				t.Fatalf("expected primary runtime identity %q, got %+v", tt.brand, evidence.RuntimeIdentities)
			}
		})
	}
}

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

func TestCraftBukkitFamilyDetector_LauncherOnlyEvidenceNotSufficient(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(dir, "META-INF", "MANIFEST.MF"),
		[]byte("Manifest-Version: 1.0\nMain-Class: io.papermc.paperclip.Main\n\n"),
	)
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(dir, "io", "papermc", "paperclip", "Main.class"),
		[]byte("launcher-only-marker"),
	)

	evidence, err := (&craftBukkitFamilyDetector{}).Detect(dir, nil, nil)
	if err != nil {
		t.Fatalf("detect launcher-only paperclip candidate: %v", err)
	}
	if evidence != nil {
		t.Fatalf("expected launcher-only evidence to remain insufficient, got %+v", evidence)
	}
}

func TestCraftBukkitFamilyDetector_Youer(t *testing.T) {
	t.Parallel()

	fixtureRoot := paperFamilyFixtureRoot(t)
	youerDir := filepath.Join(fixtureRoot, "test_youer", "youer")

	manifest, ok, err := readBukkitExecutableManifest(youerDir, nil)
	if err != nil {
		t.Fatalf("read youer manifest: %v", err)
	}
	if !ok {
		t.Fatalf("expected youer manifest")
	}
	signals := parseBukkitManifest(manifest)
	if !hasStrictYouerBukkitConfirmation(signals) {
		t.Fatalf("expected youer manifest identity to satisfy strict bukkit confirmation: %+v", signals)
	}

	evidence, err := (&craftBukkitFamilyDetector{}).Detect(youerDir, nil, nil)
	if err != nil {
		t.Fatalf("detect youer fixture: %v", err)
	}
	if evidence == nil {
		t.Fatalf("expected evidence for youer fixture")
	}
	if evidence.TopologySeed == nil {
		t.Fatalf("expected topology seed for youer fixture")
	}
	if evidence.TopologySeed.PrimaryNode != bukkitNodePaperFork {
		t.Fatalf("expected primary topology node %q, got %+v", bukkitNodePaperFork, evidence.TopologySeed)
	}
	if len(evidence.RuntimeIdentities) == 0 {
		t.Fatalf("expected runtime identities for youer fixture")
	}
	if string(evidence.RuntimeIdentities[0].Name) != "youer" {
		t.Fatalf("expected primary runtime identity youer, got %+v", evidence.RuntimeIdentities)
	}
}

func TestCraftBukkitFamilyDetector_Reaper(t *testing.T) {
	t.Parallel()

	fixtureRoot := paperFamilyFixtureRoot(t)
	reaperDir := filepath.Join(fixtureRoot, "test_reaper", "reaper")

	patchProperties, err := readBukkitExecutablePatchProperties(reaperDir, nil)
	if err != nil {
		t.Fatalf("read reaper patch.properties: %v", err)
	}
	if !hasStrictReaperBukkitConfirmation(patchProperties) {
		t.Fatalf("expected reaper patch evidence to satisfy strict bukkit confirmation: %#v", patchProperties)
	}
	if !strings.Contains(patchProperties["patch"], paperPatchReaperToken) {
		t.Fatalf("expected reaper patch marker %q in %#v", paperPatchReaperToken, patchProperties)
	}

	evidence, err := (&craftBukkitFamilyDetector{}).Detect(reaperDir, nil, nil)
	if err != nil {
		t.Fatalf("detect reaper fixture: %v", err)
	}
	if evidence == nil {
		t.Fatalf("expected evidence for reaper fixture")
	}
	if evidence.TopologySeed == nil {
		t.Fatalf("expected topology seed for reaper fixture")
	}
	if evidence.TopologySeed.PrimaryNode != bukkitNodePaperFork {
		t.Fatalf("expected primary topology node %q, got %+v", bukkitNodePaperFork, evidence.TopologySeed)
	}
	if len(evidence.RuntimeIdentities) == 0 {
		t.Fatalf("expected runtime identities for reaper fixture")
	}
	if string(evidence.RuntimeIdentities[0].Name) != "reaper" {
		t.Fatalf("expected primary runtime identity reaper, got %+v", evidence.RuntimeIdentities)
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
	if len(evidence.RuntimeIdentities) == 0 || string(evidence.RuntimeIdentities[0].Name) != "purpur" {
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
