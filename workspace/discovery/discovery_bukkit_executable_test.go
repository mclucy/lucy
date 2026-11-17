package discovery

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBukkitDetectorClassifiesPaperclip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(root, "paperclip", "META-INF", "MANIFEST.MF"),
		[]byte("Manifest-Version: 1.0\nMain-Class: org.bukkit.craftbukkit.Main\n\n"),
	)
	// Paperclip lists the Mojang API artifact and no paper-api artifact.
	writeBukkitExecutableFixtureFile(
		t,
		filepath.Join(root, "paperclip", "META-INF", "libraries.list"),
		[]byte("b8df\tcom.destroystokyo.paper:paper-mojangapi:1.21.1-R0.1-SNAPSHOT\tcom/destroystokyo/paper/paper-mojangapi/1.21.1-R0.1-SNAPSHOT/paper-mojangapi-1.21.1-R0.1-SNAPSHOT.jar\n"),
	)

	installations, _ := discoverBukkit(productFixtureScan(t, root))
	installation := singleProductInstallation(t, installations)
	if installation.Identity.Primary.Name.String() != "paper" {
		t.Fatalf(
			"expected primary runtime paper, got %+v",
			installation.Identity.Primary,
		)
	}
}

// paperLineages lists every lineage the detector can attribute. Each entry
// names a fixture directory under paper_family.
var paperLineages = map[string]bool{
	"paper":  true,
	"folia":  true,
	"divine": true,
	"purpur": true,
	"leaf":   true,
	"leaves": true,
	"reaper": true,
	"youer":  true,
}

func TestBukkitDetectorRecognizesForkLineages(t *testing.T) {
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

func TestBukkitDetectorDefersToConfirmation(t *testing.T) {
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

func TestBukkitDetectorRejectsLauncherOnly(t *testing.T) {
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

// Conflicting fork claims stay visible as separate installations instead of
// collapsing into one guessed lineage.
func TestBukkitDetectorFailsClosed(t *testing.T) {
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
