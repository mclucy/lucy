package discovery

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

func TestDiscoverForgeRecognizesLegacyUniversalLaunch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	versionDir := filepath.Join(
		root,
		"libraries",
		"net",
		"minecraftforge",
		"forge",
		"1.20.1-47.3.22",
	)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	writeFile(
		t,
		filepath.Join(versionDir, "unix_args.txt"),
		[]byte("--launchTarget forge_server"),
	)
	jarPath := filepath.Join(versionDir, "forge-1.20.1-47.3.22-universal.jar")
	writeZipFile(
		t, jarPath, map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nMain-Class: net.minecraft.server.Main\nSpecification-Title: Forge\nImplementation-Title: net.minecraftforge\nImplementation-Version: 47.3.22\nSpecification-Version: 1.20.1\n",
		},
	)

	installations := discoverForgeInstallations(t, root)
	if len(installations) != 1 {
		t.Fatalf("expected one Forge installation, got %d", len(installations))
	}

	assertForgeInstallation(
		t,
		installations[0],
		jarPath,
		"1.20.1",
		"47.3.22",
	)
	if role := installationFileRole(
		installations[0],
		filepath.Join(versionDir, "unix_args.txt"),
	); role != RoleConfiguration {
		t.Fatalf("unix args role = %q, want configuration", role)
	}
}

func TestDiscoverForgeRejectsShimOnlyLayout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	versionDir := filepath.Join(
		root,
		"libraries",
		"net",
		"minecraftforge",
		"forge",
		"1.21.11-61.1.0",
	)
	shimFiles := map[string]string{
		"META-INF/MANIFEST.MF":      "Manifest-Version: 1.0\nAutomatic-Module-Name: net.minecraftforge.bootstrap.shim\nSpecification-Title: BootStrap-Shim\nImplementation-Title: bs-shim\nImplementation-Version: 2.1.8\n",
		"bootstrap-shim.properties": "mainClass=net.minecraftforge.bootstrap.shim.Main\n",
		"bootstrap-shim.list":       "libraries/net/minecraftforge/forge/1.21.11-61.1.0/forge-1.21.11-61.1.0-server.jar\n",
	}
	writeZipFile(
		t,
		filepath.Join(versionDir, "forge-1.21.11-61.1.0-shim.jar"),
		shimFiles,
	)

	installations := discoverForgeInstallations(t, root)
	if len(installations) != 0 {
		t.Fatalf("expected shim-only layout to be rejected, got %+v", installations)
	}
}

func TestForgeUniversalJarVersionReaders(t *testing.T) {
	t.Parallel()

	jarPath := writeRootJar(
		t,
		"forge-1.20.1-47.3.22-universal.jar",
		map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nSpecification-Title: Forge\nImplementation-Title: net.minecraftforge\nImplementation-Version: 47.3.22\nSpecification-Version: 1.20.1\n",
		},
	)

	gameVersion, forgeVersion, ok := parseForgeVersionTupleFromPath(jarPath)
	if !ok {
		t.Fatalf("expected to parse legacy jar path: %s", jarPath)
	}
	if gameVersion != "1.20.1" || forgeVersion != "47.3.22" {
		t.Fatalf(
			"unexpected path parse: game=%q forge=%q",
			gameVersion,
			forgeVersion,
		)
	}
	manifestForgeVersion, manifestGameVersion := parseForgeManifestFromFile(
		t,
		jarPath,
	)
	if manifestForgeVersion != "47.3.22" {
		t.Fatalf("unexpected manifest forge version: %q", manifestForgeVersion)
	}
	if manifestGameVersion != "1.20.1" {
		t.Fatalf("unexpected manifest game version: %q", manifestGameVersion)
	}
}

func parseForgeManifestFromFile(
	t *testing.T,
	jarPath string,
) (versionpkg.Version, versionpkg.Version) {
	t.Helper()

	file, err := os.Open(jarPath)
	if err != nil {
		t.Fatalf("open jar for manifest parse: %v", err)
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("stat jar for manifest parse: %v", err)
	}

	reader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		t.Fatalf("read zip for manifest parse: %v", err)
	}

	return parseForgeManifest(reader)
}

func TestDiscoverForgeRecognizesModernArgsLayout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	versionDir := filepath.Join(
		root,
		"libraries",
		"net",
		"minecraftforge",
		"forge",
		"1.18.2-40.2.0",
	)
	serverPath := filepath.Join(versionDir, "forge-1.18.2-40.2.0-server.jar")
	writeZipFile(
		t,
		serverPath,
		map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nMain-Class: net.minecraft.server.Main\n",
		},
	)
	argsPath := filepath.Join(versionDir, "unix_args.txt")
	writeFile(
		t,
		argsPath,
		[]byte("cpw.mods.bootstraplauncher.BootstrapLauncher\n--launchTarget forgeserver\n--fml.forgeVersion 40.2.0\n--fml.mcVersion 1.18.2\n"),
	)

	installations := discoverForgeInstallations(t, root)
	if len(installations) != 1 {
		t.Fatalf("expected one Forge installation, got %d", len(installations))
	}

	assertForgeInstallation(
		t,
		installations[0],
		argsPath,
		"1.18.2",
		"40.2.0",
	)
	if role := installationFileRole(installations[0], serverPath); role != RolePayload {
		t.Fatalf("server jar role = %q, want payload", role)
	}
}

func TestDiscoverForgePrefersShimForForge61(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	versionDir := filepath.Join(
		root,
		"libraries",
		"net",
		"minecraftforge",
		"forge",
		"1.21.11-61.1.0",
	)
	serverPath := filepath.Join(versionDir, "forge-1.21.11-61.1.0-server.jar")
	writeZipFile(
		t,
		serverPath,
		map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nMain-Class: net.minecraft.server.Main\n",
		},
	)
	universalPath := filepath.Join(versionDir, "forge-1.21.11-61.1.0-universal.jar")
	writeZipFile(
		t,
		universalPath,
		map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nImplementation-Title: net.minecraftforge\nImplementation-Version: 61.1.0\nSpecification-Version: 1.21.11\n",
		},
	)
	libraryPath := filepath.Join(
		root,
		"libraries",
		"net",
		"minecraftforge",
		"JarJarFileSystems",
		"0.4.2",
		"JarJarFileSystems-0.4.2.jar",
	)
	writeZipFile(t, libraryPath, map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\n"})
	shimPath := filepath.Join(versionDir, "forge-1.21.11-61.1.0-shim.jar")
	writeZipFile(
		t,
		shimPath,
		map[string]string{
			"META-INF/MANIFEST.MF":      "Manifest-Version: 1.0\nClass-Path: libraries/net/minecraftforge/JarJarFileSystems/0.4.2/JarJarFileSystems-0.4.2.jar\nMain-Class: net.minecraftforge.bootstrap.shim.Main\n",
			"bootstrap-shim.properties": "mainClass=net.minecraftforge.bootstrap.shim.Main\n",
			"bootstrap-shim.list":       "libraries/net/minecraftforge/forge/1.21.11-61.1.0/forge-1.21.11-61.1.0-server.jar\n",
		},
	)
	writeFile(
		t,
		filepath.Join(versionDir, "unix_args.txt"),
		[]byte("-Djava.net.preferIPv6Addresses=system -jar forge-1.21.11-61.1.0-shim.jar"),
	)

	installations := discoverForgeInstallations(t, root)
	if len(installations) != 1 {
		t.Fatalf("expected one Forge installation, got %d", len(installations))
	}
	installation := installations[0]
	assertForgeInstallation(t, installation, shimPath, "1.21.11", "61.1.0")

	if role := installationFileRole(installation, universalPath); role != RoleLibrary {
		t.Fatalf("universal role = %q, want library", role)
	}
	if role := installationFileRole(installation, serverPath); role != RolePayload {
		t.Fatalf("server jar role = %q, want payload", role)
	}
	if role := installationFileRole(installation, libraryPath); role != RoleLibrary {
		t.Fatalf("referenced library role = %q, want library", role)
	}
}

func TestDiscoverForgeRejectsIncompleteLayout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	versionDir := filepath.Join(
		root,
		"libraries",
		"net",
		"minecraftforge",
		"forge",
		"1.21.11-61.1.0",
	)
	writeZipFile(
		t,
		filepath.Join(versionDir, "forge-1.21.11-61.1.0-server.jar"),
		map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\n",
		},
	)

	installations := discoverForgeInstallations(t, root)
	if len(installations) != 0 {
		t.Fatalf(
			"expected incomplete layout to be rejected, got %+v",
			installations,
		)
	}
}

func TestDiscoverForgeIgnoresCreateModJar(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRootJarIn(
		t,
		root,
		"create-1.21.1-6.0.9.jar",
		map[string]string{
			"META-INF/MANIFEST.MF":        "Manifest-Version: 1.0\nSpecification-Title: Create\nSpecification-Version: 6.0.9\nImplementation-Title: Create\nImplementation-Version: 6.0.9\nBuilt-On-Minecraft: 1.21.1\n",
			"META-INF/neoforge.mods.toml": "modLoader=\"javafml\"\nloaderVersion=\"[1,)\"\n",
		},
	)

	installations := discoverForgeInstallations(t, root)
	if len(installations) != 0 {
		t.Fatalf("expected Create mod jar to be ignored, got %+v", installations)
	}
}

func TestDiscoverForgeIgnoresRootShimJar(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRootJarIn(
		t,
		root,
		"forge-1.21.11-61.1.0-shim.jar",
		map[string]string{
			"META-INF/MANIFEST.MF":      "Manifest-Version: 1.0\nClass-Path: libraries/net/minecraftforge/JarJarFileSystems/0.4.2/JarJarFileSystems-0.4.2.jar\nAutomatic-Module-Name: net.minecraftforge.bootstrap.shim\nSpecification-Title: BootStrap-Shim\nSpecification-Version: 2.1\nImplementation-Title: bs-shim\nImplementation-Version: 2.1.8\n",
			"bootstrap-shim.properties": "mainClass=net.minecraftforge.bootstrap.shim.Main\n",
			"bootstrap-shim.list":       "libraries/net/minecraftforge/forge/1.21.11-61.1.0/forge-1.21.11-61.1.0-server.jar\n",
		},
	)

	installations := discoverForgeInstallations(t, root)
	if len(installations) != 0 {
		t.Fatalf("expected root Forge shim to be ignored, got %+v", installations)
	}
}

func discoverForgeInstallations(t *testing.T, root string) []Installation {
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

	installations, findings := discoverForge(scan)
	if len(findings) != 0 {
		t.Fatalf("unexpected Forge findings: %+v", findings)
	}
	return installations
}

func assertForgeInstallation(
	t *testing.T,
	installation Installation,
	primary string,
	gameVersion string,
	forgeVersion string,
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
	if installation.Identity.Primary.Ecosystem != catalog.EcosystemForge ||
		installation.Identity.Primary.Name != "forge" {
		t.Fatalf(
			"expected primary forge identity, got %+v",
			installation.Identity.Primary,
		)
	}
	if got := identityComponentVersion(
		installation.Identity,
		catalog.EcosystemForge,
		"forge",
	); got != forgeVersion {
		t.Fatalf("forge version mismatch: got %q want %q", got, forgeVersion)
	}
}

func identityComponentVersion(
	identity Identity,
	ecosystem catalog.Ecosystem,
	name catalog.Name,
) string {
	for _, component := range identity.Components {
		if component.Ecosystem == ecosystem && component.Name == name {
			return component.Version.String()
		}
	}
	return ""
}

func installationEntryPoints(installation Installation) []string {
	paths := make([]string, 0)
	for file := range installation.EntryPoints() {
		paths = append(paths, file.Path)
	}
	return paths
}

func installationFileRole(installation Installation, path string) FileRole {
	for _, file := range installation.Files {
		if file.Path == path {
			return file.Role
		}
	}
	return RoleUnknown
}

func writeRootJar(
	t *testing.T,
	baseName string,
	files map[string]string,
) string {
	t.Helper()

	return writeRootJarIn(t, t.TempDir(), baseName, files)
}

func writeRootJarIn(
	t *testing.T,
	root string,
	baseName string,
	files map[string]string,
) string {
	t.Helper()

	jarPath := filepath.Join(root, baseName)
	writeZipFile(t, jarPath, files)
	return jarPath
}

func writeZipFile(t *testing.T, jarPath string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(jarPath), 0o755); err != nil {
		t.Fatalf("mkdir zip dir: %v", err)
	}
	file, err := os.Create(jarPath)
	if err != nil {
		t.Fatalf("create zip file: %v", err)
	}
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir file dir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestPropertiesParsingSupportsJavaFormats(t *testing.T) {
	t.Parallel()

	data := []byte("serverJar: first.jar\nserverJar second.jar\nserverJar=custom.jar\n")
	if got := propertiesValue(data, fabricServerJarProperty); got != "custom.jar" {
		t.Fatalf("propertiesValue() = %q, want %q", got, "custom.jar")
	}
}

func TestFabricLauncherKeepsAbsolutePath(t *testing.T) {
	t.Parallel()

	primary := filepath.Join(
		string(filepath.Separator),
		"server",
		"fabric-server-launch.jar",
	)
	want := filepath.Join(string(filepath.Separator), "other", "server.jar")
	if got := fabricLauncherServerJarPath(
		primary,
		[]byte("serverJar="+want),
	); got != want {
		t.Fatalf("fabricLauncherServerJarPath() = %q, want %q", got, want)
	}
}
