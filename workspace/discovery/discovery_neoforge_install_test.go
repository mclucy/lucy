package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/catalog"
)

func TestDiscoverNeoForgeRecognizesGeneratedArgsLayout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	classicDir := filepath.Join(
		root,
		"libraries",
		"net",
		"neoforged",
		"neoforge",
		"21.4.0",
	)
	if err := os.MkdirAll(classicDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	classicJar := filepath.Join(classicDir, "neoforge-21.4.0-universal.jar")
	writeZipFile(
		t, classicJar, map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nSpecification-Title: neoforge\nClass-Path: libraries/net/neoforged/neoforge/21.4.0/neoforge-21.4.0-universal.jar\n",
		},
	)
	classicArgs := filepath.Join(classicDir, "unix_args.txt")
	writeFile(
		t,
		classicArgs,
		[]byte("--launchTarget forgeserver\n--fml.neoForgeVersion 21.4.0\n--fml.mcVersion 1.21.4\n"),
	)

	installations := discoverNeoForgeInstallations(t, root)
	if len(installations) != 1 {
		t.Fatalf("expected one NeoForge installation, got %d", len(installations))
	}
	assertNeoForgeInstallation(
		t,
		installations[0],
		classicArgs,
		"1.21.4",
		"21.4.0",
	)
	if role := installationFileRole(installations[0], classicJar); role != RoleLibrary {
		t.Fatalf("universal role = %q, want library", role)
	}

	// Year-scheme loaders keep the Minecraft triple and append a build. The
	// universal jar records the real Minecraft release, so 26.2.0.67 must not
	// become 1.26.2 or 26.2.0.
	yearDir := filepath.Join(
		root,
		"libraries",
		"net",
		"neoforged",
		"neoforge",
		"26.2.0.67",
	)
	yearJar := filepath.Join(yearDir, "neoforge-26.2.0.67-universal.jar")
	writeZipFile(
		t, yearJar, map[string]string{
			"META-INF/MANIFEST.MF":                             "Manifest-Version: 1.0\nFML-System-Mods: neoforge\n",
			"META-INF/neoforge.mods.toml":                      "modLoader=\"javafml\"\n\n[[mods]]\n    modId=\"neoforge\"\n    version=\"26.2.0.67\"\n",
			"net/neoforged/neoforge/common/version.properties": "neoforge_version=26.2.0.67\nminecraft_version=26.2\nbuild_type=STABLE\n",
		},
	)
	yearArgs := filepath.Join(yearDir, "unix_args.txt")
	writeFile(
		t,
		yearArgs,
		[]byte("net.neoforged.fml.startup.Server\n--fml.neoForgeVersion 26.2.0.67\n--fml.mcVersion 26.2\n--fml.neoFormVersion 2\n"),
	)

	installations = discoverNeoForgeInstallations(t, root)
	if len(installations) != 2 {
		t.Fatalf("expected two NeoForge installations, got %d", len(installations))
	}
	var yearInstallation *Installation
	for index := range installations {
		points := installationEntryPoints(installations[index])
		if len(points) == 1 && points[0] == yearArgs {
			yearInstallation = &installations[index]
			break
		}
	}
	if yearInstallation == nil {
		t.Fatalf("year-scheme installation not recognized: %+v", installations)
	}
	assertNeoForgeInstallation(
		t,
		*yearInstallation,
		yearArgs,
		"26.2",
		"26.2.0.67",
	)
	if role := installationFileRole(*yearInstallation, yearJar); role != RoleLibrary {
		t.Fatalf("universal role = %q, want library", role)
	}
}

func discoverNeoForgeInstallations(
	t *testing.T,
	root string,
) []Installation {
	t.Helper()

	scan, err := newScan(root)
	if err != nil {
		t.Fatalf("create scan: %v", err)
	}
	defer func() {
		if err := scan.Close(); err != nil {
			t.Fatalf("close scan: %v", err)
		}
	}()

	installations, findings := discoverNeoForge(scan)
	if len(findings) != 0 {
		t.Fatalf("unexpected NeoForge findings: %+v", findings)
	}
	return installations
}

func assertNeoForgeInstallation(
	t *testing.T,
	installation Installation,
	primary string,
	gameVersion string,
	loaderVersion string,
) {
	t.Helper()

	if got := installationEntryPoints(installation); len(got) != 1 ||
		got[0] != primary {
		t.Fatalf("entry points = %v, want [%s]", got, primary)
	}
	if got := identityComponentVersion(
		installation.Identity,
		catalog.EcosystemMinecraft,
		"minecraft",
	); got != gameVersion {
		t.Fatalf("game version mismatch: got %q want %q", got, gameVersion)
	}
	if installation.Identity.Primary.Ecosystem != catalog.EcosystemNeoForge ||
		installation.Identity.Primary.Name != "neoforge" {
		t.Fatalf(
			"expected primary neoforge identity, got %+v",
			installation.Identity.Primary,
		)
	}
	if got := identityComponentVersion(
		installation.Identity,
		catalog.EcosystemNeoForge,
		"neoforge",
	); got != loaderVersion {
		t.Fatalf(
			"neoforge version mismatch: got %q want %q",
			got,
			loaderVersion,
		)
	}
}
