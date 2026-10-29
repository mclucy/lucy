package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
)

const vanillaVersionJSON = `{"id":"1.20.1","name":"1.20.1"}`

// fabricMetaLauncherFiles reproduces the entries a Fabric meta launcher
// carries before its first run: the launcher main class and the unattended
// install properties that name the loader and game versions.
func fabricMetaLauncherFiles(loader, minecraft string) map[string]string {
	return map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nMain-Class: net.fabricmc.installer.ServerLauncher\n\n",
		"install.properties":   "fabric-loader-version=" + loader + "\ngame-version=" + minecraft + "\n",
	}
}

func vanillaInstallationFiles() map[string]string {
	return map[string]string{"version.json": vanillaVersionJSON}
}

// Two copies of one real runtime are one installation. Its entry points stay
// observable, and later selection picks the lexical first one because the
// declaration records identity, not a path.
func TestDiscoverMergesDuplicateVanillaJars(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	primary := writeRootJarIn(t, root, "server.jar", vanillaInstallationFiles())
	backup := writeRootJarIn(t, root, "server-backup.jar", vanillaInstallationFiles())

	result := Discover(root)
	if len(result.Installations) != 1 {
		t.Fatalf(
			"installations = %d, want the two copies merged into one",
			len(result.Installations),
		)
	}
	installation := result.Installations[0]
	paths := entryPointPaths(installation)
	slices.Sort(paths)
	want := []string{backup, primary}
	if !slices.Equal(paths, want) {
		t.Fatalf("entry points = %v, want %v", paths, want)
	}
	if got := installation.Identity.MinecraftVersion().String(); got != "1.20.1" {
		t.Fatalf("Minecraft version = %q, want 1.20.1", got)
	}

	if err := result.Select(Request{}); err != nil {
		t.Fatalf("Select inferred installation: %v", err)
	}
	if result.Selection.EntryPoint == nil ||
		result.Selection.EntryPoint.Path != backup {
		t.Fatalf(
			"selected entry point = %+v, want the lexical first copy %s",
			result.Selection.EntryPoint,
			backup,
		)
	}
}

// A symlinked alias and its target are one physical installation. Selecting
// the alias resolves to the canonical file, which is what later observation
// reports.
func TestDiscoverResolvesPaperSymlinkAlias(t *testing.T) {
	t.Parallel()

	source := writeFixtureJar(
		t,
		paperFamilyFixturePath(t, "test_paper", "paper"),
		"paper-1.21.11-130.jar",
	)
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read paper fixture: %v", err)
	}
	root := t.TempDir()
	paperPath := filepath.Join(root, "paper-1.21.11-130.jar")
	if err := os.WriteFile(paperPath, data, 0o644); err != nil {
		t.Fatalf("write paper jar: %v", err)
	}
	aliasPath := filepath.Join(root, "server.jar")
	if err := os.Symlink(paperPath, aliasPath); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("file symlinks unavailable: %v", err)
		}
		t.Fatalf("create alias: %v", err)
	}

	result := Discover(root)
	if len(result.Installations) != 1 {
		t.Fatalf(
			"installations = %d, want the alias and its target to share one",
			len(result.Installations),
		)
	}
	identity := result.Installations[0].Identity
	if got := identity.Primary.Name.String(); got != "paper" {
		t.Fatalf("primary runtime = %q, want paper", got)
	}
	if got := identity.MinecraftVersion().String(); got != "1.21.11" {
		t.Fatalf("Minecraft version = %q, want 1.21.11", got)
	}
	if got := entryPointPaths(result.Installations[0]); !slices.Equal(got, []string{paperPath}) {
		t.Fatalf("entry points = %v, want the canonical jar %s", got, paperPath)
	}

	if err := result.Select(Request{EntryPoint: aliasPath}); err != nil {
		t.Fatalf("Select alias entry point: %v", err)
	}
	if result.Selection.EntryPoint == nil ||
		result.Selection.EntryPoint.Path != paperPath {
		t.Fatalf(
			"selected entry point = %+v, want the canonical %s",
			result.Selection.EntryPoint,
			paperPath,
		)
	}
}

// A vanilla jar copied under Maven-shaped loader paths stays one plain
// Minecraft installation; loader detectors do not claim artifacts they cannot
// prove belong to an installed loader layout.
func TestDiscoverKeepsVanillaIdentityUnderLoaderServerPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	serverPath := writeRootJarIn(t, root, "server.jar", vanillaInstallationFiles())
	writeRootJarIn(
		t,
		root,
		filepath.Join(
			"libraries", "net", "minecraftforge", "forge",
			"1.21.1-52.0.28", "forge-1.21.1-52.0.28-server.jar",
		),
		vanillaInstallationFiles(),
	)
	writeRootJarIn(
		t,
		root,
		filepath.Join(
			"libraries", "net", "neoforged", "neoforge",
			"21.1.248", "neoforge-21.1.248-server.jar",
		),
		vanillaInstallationFiles(),
	)

	result := Discover(root)
	if len(result.Installations) != 1 {
		t.Fatalf(
			"installations = %d, want only the root vanilla jar",
			len(result.Installations),
		)
	}
	installation := result.Installations[0]
	if got := installation.Identity.Primary.Name.String(); got != "minecraft" {
		t.Fatalf("primary runtime = %q, want minecraft", got)
	}
	if got := installation.Identity.Ecosystem(); got != catalog.EcosystemMinecraft {
		t.Fatalf("ecosystem = %q, want minecraft", got)
	}
	if err := result.Select(Request{}); err != nil {
		t.Fatalf("Select inferred installation: %v", err)
	}
	if got := result.Selection.EntryPoint.Path; got != serverPath {
		t.Fatalf("selected entry point = %q, want %q", got, serverPath)
	}
}

// A Fabric meta launcher declares its loader and game identity before the
// first run, when no generated shim or payload exists yet.
func TestDiscoverFabricMetaLauncherIdentity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	launcherPath := writeRootJarIn(
		t,
		root,
		"fabric-server-mc.1.21.1-loader.0.19.5-launcher.1.1.2.jar",
		fabricMetaLauncherFiles("0.19.5", "1.21.1"),
	)

	result := Discover(root)
	if len(result.Installations) != 1 {
		t.Fatalf("installations = %d, want one Fabric installation", len(result.Installations))
	}
	installation := result.Installations[0]
	if got := installation.Identity.Primary.Name.String(); got != "fabric" {
		t.Fatalf("primary runtime = %q, want fabric", got)
	}
	if got := installation.Identity.Primary.Version.String(); got != "0.19.5" {
		t.Fatalf("loader version = %q, want 0.19.5", got)
	}
	if got := installation.Identity.MinecraftVersion().String(); got != "1.21.1" {
		t.Fatalf("Minecraft version = %q, want 1.21.1", got)
	}
	if err := result.Select(Request{}); err != nil {
		t.Fatalf("Select inferred installation: %v", err)
	}
	if got := result.Selection.EntryPoint.Path; got != launcherPath {
		t.Fatalf("selected entry point = %q, want %q", got, launcherPath)
	}
}
