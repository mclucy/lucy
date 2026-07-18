package detector

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestDetectNeoForgeInstallPrefersHashVerifiedUniversalJar(t *testing.T) {
	t.Parallel()

	versionDir := filepath.Join(
		t.TempDir(),
		"libraries",
		"net",
		"neoforged",
		"neoforge",
		"21.4.0",
	)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	universalJar := filepath.Join(versionDir, "neoforge-21.4.0-universal.jar")
	writeFile(t, universalJar, []byte("official-universal"))
	writeFile(
		t,
		filepath.Join(versionDir, "neoforge-21.4.0-server.jar"),
		[]byte("official-server"),
	)

	restore := stubNeoforgeArtifactHashLookup(
		func(
			version string,
			artifact modLoaderArtifactKind,
			filePath string,
		) (bool, error) {
			return artifact == modLoaderArtifactUniversal && filepath.Base(filePath) == filepath.Base(universalJar), nil
		},
	)
	defer restore()

	runtimes := NeoForgeInstallationRuntimes(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(versionDir))))))
	if len(runtimes) != 1 {
		t.Fatalf(
			"expected one hash-verified NeoForge install runtime, got %d",
			len(runtimes),
		)
	}

	assertNeoForgeRuntime(
		t,
		runtimes[0],
		universalJar,
		"1.21.4",
		"21.4.0",
	)
}

func TestDetectNeoForgeInstallFallsBackToUnpackVerification(t *testing.T) {
	t.Parallel()

	workPath := t.TempDir()
	versionDir := filepath.Join(
		workPath,
		"libraries",
		"net",
		"neoforged",
		"neoforge",
		"21.4.0",
	)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	jarPath := filepath.Join(versionDir, "neoforge-21.4.0-universal.jar")
	writeZipFile(
		t, jarPath, map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nSpecification-Title: neoforge\nClass-Path: libraries/net/neoforged/neoforge/21.4.0/neoforge-21.4.0-universal.jar\n",
		},
	)

	restore := stubNeoforgeArtifactHashLookup(
		func(
			version string,
			artifact modLoaderArtifactKind,
			filePath string,
		) (bool, error) {
			return false, nil
		},
	)
	defer restore()

	runtimes := NeoForgeInstallationRuntimes(workPath)
	if len(runtimes) != 1 {
		t.Fatalf(
			"expected unpack fallback to identify NeoForge install, got %d runtimes",
			len(runtimes),
		)
	}

	assertNeoForgeRuntime(
		t,
		runtimes[0],
		jarPath,
		"1.21.4",
		"21.4.0",
	)
}

func TestDetectNeoForgeInstallRejectsShimOnly(t *testing.T) {
	t.Parallel()

	workPath := t.TempDir()
	versionDir := filepath.Join(
		workPath,
		"libraries",
		"net",
		"neoforged",
		"neoforge",
		"21.4.0",
	)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	writeZipFile(
		t,
		filepath.Join(versionDir, "neoforge-21.4.0-shim.jar"),
		map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nSpecification-Title: neoforge\n",
		},
	)

	restore := stubNeoforgeArtifactHashLookup(
		func(
			version string,
			artifact modLoaderArtifactKind,
			filePath string,
		) (bool, error) {
			return artifact == modLoaderArtifactShim, nil
		},
	)
	defer restore()

	runtimes := NeoForgeInstallationRuntimes(workPath)
	if len(runtimes) != 0 {
		t.Fatalf(
			"expected shim-only layout to be rejected, got %d runtimes",
			len(runtimes),
		)
	}
}

func assertNeoForgeRuntime(
	t *testing.T,
	runtime *ExecutableEvidence,
	primary string,
	gameVersion string,
	loaderVersion string,
) {
	t.Helper()

	wantGameVersion := types.BareVersion(gameVersion)

	if runtime.PrimaryPath != primary {
		t.Fatalf(
			"primary path mismatch: got %q want %q",
			runtime.PrimaryPath,
			primary,
		)
	}
	if got := runtimeIdentityVersion(
		runtime,
		types.EcoMinecraft,
		"minecraft",
	); got != wantGameVersion.String() {
		t.Fatalf(
			"game version mismatch: got %q want %q",
			got,
			wantGameVersion,
		)
	}
	if runtime.PrimaryRuntime == nil ||
		runtime.PrimaryRuntime.Eco != types.EcoNeoforge ||
		runtime.PrimaryRuntime.Name != "neoforge" {
		t.Fatalf(
			"expected primary neoforge runtime, got %+v",
			runtime.PrimaryRuntime,
		)
	}
	if got := runtimeIdentityVersion(
		runtime,
		types.EcoNeoforge,
		"neoforge",
	); got != loaderVersion {
		t.Fatalf(
			"neoforge version mismatch: got %q want %q",
			got,
			loaderVersion,
		)
	}
}

func stubNeoforgeArtifactHashLookup(
	fn func(version string, artifact modLoaderArtifactKind, filePath string) (
		bool,
		error,
	),
) func() {
	stubMu.Lock()
	original := neoforgeArtifactHashLookup
	neoforgeArtifactHashLookup = fn
	return func() {
		neoforgeArtifactHashLookup = original
		stubMu.Unlock()
	}
}

var stubMu sync.Mutex
