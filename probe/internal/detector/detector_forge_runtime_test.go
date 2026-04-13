package detector

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestDetectForgeInstallFromVersionDirPrefersHashVerifiedServerJar(t *testing.T) {
	t.Parallel()

	versionDir := filepath.Join(
		t.TempDir(),
		"libraries",
		"net",
		"minecraftforge",
		"forge",
		"1.21.11-61.1.0",
	)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	serverJar := filepath.Join(versionDir, "forge-1.21.11-61.1.0-server.jar")
	writeFile(t, serverJar, []byte("official-server"))
	writeFile(t, filepath.Join(versionDir, "forge-1.21.11-61.1.0-universal.jar"), []byte("official-universal"))

	restore := stubForgeArtifactHashLookup(func(version string, artifact forgeArtifactKind, filePath string) (bool, error) {
		return artifact == forgeArtifactServer && filepath.Base(filePath) == filepath.Base(serverJar), nil
	})
	defer restore()

	runtime, err := detectForgeInstallFromVersionDir(versionDir)
	if err != nil {
		t.Fatalf("detect forge install: %v", err)
	}
	if runtime == nil {
		t.Fatalf("expected hash-verified Forge install runtime")
	}

	assertForgeRuntime(t, runtime, serverJar, "1.21.11", "61.1.0")
}

func TestDetectForgeInstallFromVersionDirFallsBackToUnpackVerification(t *testing.T) {
	t.Parallel()

	versionDir := filepath.Join(
		t.TempDir(),
		"libraries",
		"net",
		"minecraftforge",
		"forge",
		"1.20.1-47.3.22",
	)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	writeFile(t, filepath.Join(versionDir, "unix_args.txt"), []byte("--launchTarget forge_server"))
	jarPath := filepath.Join(versionDir, "forge-1.20.1-47.3.22-universal.jar")
	writeZipFile(t, jarPath, map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nSpecification-Title: Forge\nImplementation-Title: net.minecraftforge\nImplementation-Version: 47.3.22\nSpecification-Version: 1.20.1\n",
	})

	restore := stubForgeArtifactHashLookup(func(version string, artifact forgeArtifactKind, filePath string) (bool, error) {
		return false, nil
	})
	defer restore()

	runtime, err := detectForgeInstallFromVersionDir(versionDir)
	if err != nil {
		t.Fatalf("detect forge install with unpack fallback: %v", err)
	}
	if runtime == nil {
		t.Fatalf("expected unpack fallback to identify Forge install")
	}

	assertForgeRuntime(t, runtime, jarPath, "1.20.1", "47.3.22")
}

func TestDetectForgeInstallFromVersionDirRejectsShimOnlyLayout(t *testing.T) {
	t.Parallel()

	versionDir := filepath.Join(
		t.TempDir(),
		"libraries",
		"net",
		"minecraftforge",
		"forge",
		"1.21.11-61.1.0",
	)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	writeZipFile(t, filepath.Join(versionDir, "forge-1.21.11-61.1.0-shim.jar"), map[string]string{
		"META-INF/MANIFEST.MF":      "Manifest-Version: 1.0\nAutomatic-Module-Name: net.minecraftforge.bootstrap.shim\nSpecification-Title: BootStrap-Shim\nImplementation-Title: bs-shim\nImplementation-Version: 2.1.8\n",
		"bootstrap-shim.properties": "mainClass=net.minecraftforge.bootstrap.shim.Main\n",
	})

	restore := stubForgeArtifactHashLookup(func(version string, artifact forgeArtifactKind, filePath string) (bool, error) {
		return artifact == forgeArtifactShim, nil
	})
	defer restore()

	runtime, err := detectForgeInstallFromVersionDir(versionDir)
	if err != nil {
		t.Fatalf("detect shim-only install: %v", err)
	}
	if runtime != nil {
		t.Fatalf("expected shim-only layout to be rejected, got %+v", runtime)
	}
}

func TestForgeLegacyDetectorDetectsUniversalJar(t *testing.T) {
	t.Parallel()

	jarPath := filepath.Join(
		testDataRoot(t),
		"forge",
		"forge-1.20.1-47.3.22-universal.jar",
	)

	gameVersion, forgeVersion, ok := parseForgeVersionTupleFromPath(jarPath)
	if !ok {
		t.Fatalf("expected to parse legacy jar path: %s", jarPath)
	}
	if gameVersion != types.RawVersion("1.20.1") || forgeVersion != types.RawVersion("47.3.22") {
		t.Fatalf(
			"unexpected path parse: game=%q forge=%q",
			gameVersion,
			forgeVersion,
		)
	}
	manifestForgeVersion, manifestGameVersion := parseForgeManifestFromFile(t, jarPath)
	if manifestForgeVersion != types.RawVersion("47.3.22") {
		t.Fatalf("unexpected manifest forge version: %q", manifestForgeVersion)
	}
	if manifestGameVersion != types.RawVersion("1.20.1") {
		t.Fatalf("unexpected manifest game version: %q", manifestGameVersion)
	}

	runtime := detectForgeRuntimeWith(t, &forgeLegacyDetector{}, jarPath)
	if runtime == nil {
		t.Fatalf("expected legacy Forge detector to detect %s", jarPath)
	}
	if runtime.GameVersion.CanInfer() || runtime.GameVersion.IsInvalid() {
		t.Fatalf(
			"legacy detector returned unresolved game version: game=%q raw=%#v",
			runtime.GameVersion,
			runtime,
		)
	}

	assertForgeRuntime(t, runtime, jarPath, "1.20.1", "47.3.22")
}

func parseForgeManifestFromFile(
	t *testing.T,
	jarPath string,
) (types.RawVersion, types.RawVersion) {
	t.Helper()

	file, err := os.Open(jarPath)
	if err != nil {
		t.Fatalf("open jar for manifest parse: %v", err)
	}
	defer file.Close()

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

func TestForgeModernDetectorDetectsLibraryLayout(t *testing.T) {
	t.Parallel()

	jarPath := writeTestJar(
		t,
		"1.18.2-40.2.0",
		"forge-1.18.2-40.2.0-server.jar",
		map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nMain-Class: net.minecraft.server.Main\n",
		},
	)
	mkdirAll(t, filepath.Join(filepath.Dir(jarPath), "unix_args.txt"))
	mkdirAll(t, filepath.Join(filepath.Dir(jarPath), "win_args.txt"))

	runtime := detectForgeRuntimeWith(t, &forgeModernDetector{}, jarPath)
	if runtime == nil {
		t.Fatalf("expected modern Forge detector to detect %s", jarPath)
	}

	assertForgeRuntime(t, runtime, jarPath, "1.18.2", "40.2.0")
}

func TestForgeLatestDetectorDetectsForge61ServerJar(t *testing.T) {
	t.Parallel()

	jarPath := writeTestJar(
		t,
		"1.21.11-61.1.0",
		"forge-1.21.11-61.1.0-server.jar",
		map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nMain-Class: net.minecraft.server.Main\n",
		},
	)
	writeFile(t, filepath.Join(filepath.Dir(jarPath), "forge-1.21.11-61.1.0-shim.jar"), []byte("shim"))
	writeFile(t, filepath.Join(filepath.Dir(jarPath), "unix_args.txt"), []byte("--launchTarget forge_server"))

	runtime := detectForgeRuntimeWith(t, &forgeLatestDetector{}, jarPath)
	if runtime == nil {
		t.Fatalf("expected latest Forge detector to detect %s", jarPath)
	}

	assertForgeRuntime(t, runtime, jarPath, "1.21.11", "61.1.0")
}

func TestForgeLatestDetectorRejectsIncompleteLayout(t *testing.T) {
	t.Parallel()

	jarPath := writeTestJar(
		t,
		"1.21.11-61.1.0",
		"forge-1.21.11-61.1.0-server.jar",
		map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nMain-Class: net.minecraft.server.Main\n",
		},
	)

	runtime := detectForgeRuntimeWith(t, &forgeLatestDetector{}, jarPath)
	if runtime != nil {
		t.Fatalf("expected latest Forge detector to reject incomplete layout, got %+v", runtime)
	}
}

func TestExecutableRejectsCreateModJar(t *testing.T) {
	t.Parallel()

	jarPath := writeRootJar(
		t,
		"create-1.21.1-6.0.9.jar",
		map[string]string{
			"META-INF/MANIFEST.MF":        "Manifest-Version: 1.0\nSpecification-Title: Create\nSpecification-Version: 6.0.9\nImplementation-Title: Create\nImplementation-Version: 6.0.9\nBuilt-On-Minecraft: 1.21.1\n",
			"META-INF/neoforge.mods.toml": "modLoader=\"javafml\"\nloaderVersion=\"[1,)\"\n",
		},
	)

	runtime := Executable(jarPath)
	if runtime != types.NoExecutable {
		t.Fatalf("expected Create mod jar to be rejected as executable, got %+v", runtime)
	}
}

func TestExecutableRejectsForgeShimJar(t *testing.T) {
	t.Parallel()

	jarPath := writeRootJar(
		t,
		"forge-1.21.11-61.1.0-shim.jar",
		map[string]string{
			"META-INF/MANIFEST.MF":      "Manifest-Version: 1.0\nClass-Path: libraries/net/minecraftforge/JarJarFileSystems/0.4.2/JarJarFileSystems-0.4.2.jar\nAutomatic-Module-Name: net.minecraftforge.bootstrap.shim\nSpecification-Title: BootStrap-Shim\nSpecification-Version: 2.1\nImplementation-Title: bs-shim\nImplementation-Version: 2.1.8\n",
			"bootstrap-shim.properties": "mainClass=net.minecraftforge.bootstrap.shim.Main\n",
			"bootstrap-shim.list":       "libraries/net/minecraftforge/forge/1.21.11-61.1.0/forge-1.21.11-61.1.0-server.jar\n",
		},
	)

	runtime := Executable(jarPath)
	if runtime != types.NoExecutable {
		t.Fatalf("expected Forge shim jar to be rejected as executable, got %+v", runtime)
	}
}

func detectForgeRuntimeWith(
	t *testing.T,
	detector ExecutableDetector,
	jarPath string,
) *types.RuntimeInfo {
	t.Helper()

	file, err := os.Open(jarPath)
	if err != nil {
		t.Fatalf("open jar: %v", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("stat jar: %v", err)
	}

	reader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}

	runtime, err := detector.Detect(jarPath, reader, file)
	if err != nil {
		t.Fatalf("detect runtime: %v", err)
	}

	return runtime
}

func assertForgeRuntime(
	t *testing.T,
	runtime *types.RuntimeInfo,
	primary string,
	gameVersion string,
	forgeVersion string,
) {
	t.Helper()

	wantGameVersion := types.RawVersion(gameVersion)

	if runtime.PrimaryEntrance != primary {
		t.Fatalf("primary entrance mismatch: got %q want %q", runtime.PrimaryEntrance, primary)
	}
	if runtime.GameVersion != wantGameVersion {
		t.Fatalf(
			"game version mismatch: got raw=%q string=%q want raw=%q string=%q",
			runtime.GameVersion,
			runtime.GameVersion.String(),
			wantGameVersion,
			wantGameVersion.String(),
		)
	}
	if got := runtime.DerivedModLoader(); got != types.PlatformForge {
		t.Fatalf("derived mod loader mismatch: got %s want %s", got, types.PlatformForge)
	}
	if got := runtime.DerivedLoaderVersion(); got != forgeVersion {
		t.Fatalf("forge version mismatch: got %q want %q", got, forgeVersion)
	}
}

func writeTestJar(
	t *testing.T,
	versionDir string,
	baseName string,
	files map[string]string,
) string {
	t.Helper()

	root := t.TempDir()
	jarPath := filepath.Join(
		root,
		"libraries",
		"net",
		"minecraftforge",
		"forge",
		versionDir,
		baseName,
	)
	if err := os.MkdirAll(filepath.Dir(jarPath), 0o755); err != nil {
		t.Fatalf("mkdir jar dir: %v", err)
	}

	file, err := os.Create(jarPath)
	if err != nil {
		t.Fatalf("create jar: %v", err)
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
		t.Fatalf("close jar file: %v", err)
	}

	return jarPath
}

func writeRootJar(t *testing.T, baseName string, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	jarPath := filepath.Join(root, baseName)
	file, err := os.Create(jarPath)
	if err != nil {
		t.Fatalf("create root jar: %v", err)
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
		t.Fatalf("close root zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close root jar file: %v", err)
	}

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

func stubForgeArtifactHashLookup(
	fn func(version string, artifact forgeArtifactKind, filePath string) (bool, error),
) func() {
	original := forgeArtifactHashLookup
	forgeArtifactHashLookup = fn
	return func() {
		forgeArtifactHashLookup = original
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

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir path: %v", err)
	}
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("touch file: %v", err)
	}
}
