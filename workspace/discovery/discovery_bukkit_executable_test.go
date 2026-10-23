package discovery

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCraftBukkitFamilyDetector_PaperFixtureClassifiesAsPaper(t *testing.T) {
	t.Parallel()

	fixtureRoot := paperFamilyFixtureRoot(t)
	scan := productFixtureScan(t, filepath.Join(fixtureRoot, "test_paper"))

	installations, _ := discoverBukkit(scan)
	installation := singleProductInstallation(t, installations)
	if installation.Identity.Primary.Name.String() != "paper" {
		t.Fatalf(
			"expected primary runtime paper, got %+v",
			installation.Identity.Primary,
		)
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
		name           string
		root           string
		wantIdentities []string
	}{
		{
			name:           "paper fixture projects official paper runtime",
			root:           filepath.Join(fixtureRoot, "test_paper"),
			wantIdentities: []string{"paper"},
		},
		{
			name:           "paper fork fixture projects paper fork runtime",
			root:           filepath.Join(fixtureRoot, "test_folia"),
			wantIdentities: []string{"folia"},
		},
		{
			name:           "contradiction preserves both claims",
			root:           filepath.Dir(contradictionDir),
			wantIdentities: []string{"paper", "purpur"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			installations, _ := discoverBukkit(productFixtureScan(t, tt.root))
			if got := productInstallationNames(installations); !slices.Equal(
				got,
				tt.wantIdentities,
			) {
				t.Fatalf(
					"expected identities %v, got %v",
					tt.wantIdentities,
					got,
				)
			}
		})
	}
}

func TestCraftBukkitFamilyDetector_KnownPaperForkLineages(t *testing.T) {
	t.Parallel()

	fixtureRoot := paperFamilyFixtureRoot(t)
	for lineage := range paperLineages {
		t.Run(lineage, func(t *testing.T) {
			t.Parallel()

			installations, _ := discoverBukkit(
				productFixtureScan(
					t,
					filepath.Join(fixtureRoot, "test_"+lineage),
				),
			)
			installation := singleProductInstallation(t, installations)
			if installation.Identity.Primary.Name.String() != lineage {
				t.Fatalf(
					"expected primary runtime %q, got %+v",
					lineage,
					installation.Identity.Primary,
				)
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

	installations, _ := discoverBukkit(productFixtureScan(t, filepath.Dir(dir)))
	if len(installations) != 0 {
		t.Fatalf(
			"expected no installation without bukkit confirmation, got %v",
			productInstallationNames(installations),
		)
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

	// Bukkit confirmation gates every later stage. Without it, Paper-family
	// markers must not produce an installation.
	installations, _ := discoverBukkit(productFixtureScan(t, filepath.Dir(dir)))
	if len(installations) != 0 {
		t.Fatalf(
			"expected no installation before confirmation, got %v",
			productInstallationNames(installations),
		)
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

	installations, _ := discoverBukkit(productFixtureScan(t, filepath.Dir(dir)))
	if len(installations) != 0 {
		t.Fatalf(
			"expected launcher-only evidence to remain insufficient, got %v",
			productInstallationNames(installations),
		)
	}
}

func TestCraftBukkitFamilyDetector_Youer(t *testing.T) {
	t.Parallel()

	fixtureRoot := paperFamilyFixtureRoot(t)
	scan := productFixtureScan(t, filepath.Join(fixtureRoot, "test_youer"))
	primary, ok := scan.File("youer")
	if !ok {
		t.Fatalf("expected youer fixture artifact")
	}

	manifest, ok, err := readBukkitManifest(scan, primary)
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

	installations, _ := discoverBukkit(scan)
	installation := singleProductInstallation(t, installations)
	if installation.Identity.Primary.Name.String() != "youer" {
		t.Fatalf(
			"expected primary runtime youer, got %+v",
			installation.Identity.Primary,
		)
	}
}

func TestCraftBukkitFamilyDetector_Reaper(t *testing.T) {
	t.Parallel()

	fixtureRoot := paperFamilyFixtureRoot(t)
	scan := productFixtureScan(t, filepath.Join(fixtureRoot, "test_reaper"))
	primary, ok := scan.File("reaper")
	if !ok {
		t.Fatalf("expected reaper fixture artifact")
	}

	patchProperties, err := readBukkitPatchProperties(scan, primary)
	if err != nil {
		t.Fatalf("read reaper patch.properties: %v", err)
	}
	if !hasStrictReaperBukkitConfirmation(patchProperties) {
		t.Fatalf("expected reaper patch evidence to satisfy strict bukkit confirmation: %#v", patchProperties)
	}
	if !strings.Contains(patchProperties["patch"], paperPatchReaperToken) {
		t.Fatalf("expected reaper patch marker %q in %#v", paperPatchReaperToken, patchProperties)
	}

	installations, _ := discoverBukkit(scan)
	installation := singleProductInstallation(t, installations)
	if installation.Identity.Primary.Name.String() != "reaper" {
		t.Fatalf(
			"expected primary runtime reaper, got %+v",
			installation.Identity.Primary,
		)
	}
}

func TestCraftBukkitFamilyDetector_FamilyMissStillRunsLineageRules(t *testing.T) {
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

	judgment := paperJudgment{}
	judgment.bukkitConfirmed = true
	judgment.observations = paperObservations{
		librariesListEntries: []string{"org.purpurmc.purpur:purpur-api:1.21.11-R0.1-SNAPSHOT"},
	}
	reasonPaperFamily(&judgment)
	if judgment.familyResult != familyMiss {
		t.Fatalf("expected family miss before lineage rules, got %v", judgment.familyResult)
	}

	installations, _ := discoverBukkit(productFixtureScan(t, filepath.Dir(dir)))
	installation := singleProductInstallation(t, installations)
	if installation.Identity.Primary.Name.String() != "purpur" {
		t.Fatalf(
			"expected lineage recovery to select purpur, got %+v",
			installation.Identity.Primary,
		)
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

	judgment := paperJudgment{}
	judgment.bukkitConfirmed = true
	judgment.observations = paperObservations{
		librariesListEntries: []string{
			"io.papermc.paper:paper-api:1.21.11-R0.1-SNAPSHOT",
			"org.purpurmc.purpur:purpur-api:1.21.11-R0.1-SNAPSHOT",
		},
	}
	reasonPaperFamily(&judgment)
	attributePaperLineage(&judgment)
	resolvePaperContradictions(&judgment)
	if judgment.familyResult != familyContradiction {
		t.Fatalf("expected contradictory lineages to set family contradiction, got %v", judgment.familyResult)
	}
	if !judgment.contradicted {
		t.Fatalf("expected contradictory state to be retained")
	}

	installations, _ := discoverBukkit(productFixtureScan(t, filepath.Dir(dir)))
	if got := productInstallationNames(installations); !slices.Equal(
		got,
		[]string{"paper", "purpur"},
	) {
		t.Fatalf(
			"expected both contradictory claims preserved, got %v",
			got,
		)
	}
}

func productFixtureScan(t *testing.T, root string) *scan {
	t.Helper()

	s, err := newScan(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func productInstallationNames(installations []Installation) []string {
	names := make([]string, 0, len(installations))
	for _, installation := range installations {
		names = append(
			names,
			installation.Identity.Primary.Name.String(),
		)
	}
	slices.Sort(names)
	return names
}

func singleProductInstallation(
	t *testing.T,
	installations []Installation,
) Installation {
	t.Helper()

	if len(installations) != 1 {
		t.Fatalf(
			"expected one installation, got %d (%v)",
			len(installations),
			productInstallationNames(installations),
		)
	}
	return installations[0]
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
