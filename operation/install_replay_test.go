package operation

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/version"
)

func isolateOperationHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
}

func operationTestWrite(t *testing.T, root, relative string, data []byte, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", relative, err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

func operationTestRead(t *testing.T, root, relative string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return data
}

func operationTestDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func operationTestMaterializer(t *testing.T, files map[string][]byte) func(*catalog.Artifact) (artifact.MaterializedFile, error) {
	t.Helper()
	directory := t.TempDir()
	return func(a *catalog.Artifact) (artifact.MaterializedFile, error) {
		data, ok := files[a.Filename]
		if !ok {
			return artifact.MaterializedFile{}, fmt.Errorf("unexpected artifact %s", a.Filename)
		}
		path := filepath.Join(directory, a.Filename)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return artifact.MaterializedFile{}, err
		}
		return artifact.MaterializedFile{
			Path:   path,
			Hashes: map[string]string{"sha256": operationTestDigest(data)},
		}, nil
	}
}

func operationTestMiniVanillaJar(t *testing.T, id string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("version.json")
	if err != nil {
		t.Fatalf("create version.json: %v", err)
	}
	if _, err := entry.Write([]byte(`{"id":"` + id + `","name":"` + id + `"}`)); err != nil {
		t.Fatalf("write version.json: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("seal miniature jar: %v", err)
	}
	return buffer.Bytes()
}

// replaySource answers the initialization and installation capabilities from
// one pinned vanilla artifact and no catalogue packages.
type replaySource struct {
	core catalog.Artifact
}

func (s *replaySource) Candidates(
	_ context.Context,
	_ catalog.Registry,
	_, _ string,
	_ catalog.Ecosystem,
) ([]upstream.Candidate, error) {
	return nil, nil
}

func (s *replaySource) GetArtifactMapper(catalog.Provider) (upstream.ArtifactMapProvider, bool, error) {
	return nil, false, nil
}

func (s *replaySource) IdentifyFillBuild(
	_ context.Context,
	_ string,
	_ version.Version,
	_ string,
) (version.Version, bool, error) {
	return "", false, nil
}

func (s *replaySource) IdentifyPurpurBuild(
	_ context.Context,
	_ version.Version,
	_ string,
) (version.Version, bool, error) {
	return "", false, nil
}

func (s *replaySource) ResolveArtifactByHash(
	_ context.Context,
	_ upstream.ArtifactFingerprint,
) routing.ArtifactLookup {
	return routing.ArtifactLookup{}
}

func (s *replaySource) Metadata(_ context.Context, endpoint string, _ any) error {
	return fmt.Errorf("unexpected metadata request for %s", endpoint)
}

func (s *replaySource) ResolveServerProduct(
	_ context.Context,
	request routing.ServerProductRequest,
	_ func(*catalog.Artifact) (artifact.MaterializedFile, error),
) (routing.ServerProductResolution, error) {
	coreVersion := request.ProductVersion
	if coreVersion == "" || coreVersion.CanInfer() || coreVersion.IsInvalid() {
		coreVersion = "1.21.4"
	}
	artifact := s.core
	artifact.Version = coreVersion
	return routing.ServerProductResolution{
		Minecraft:      request.Minecraft,
		Product:        request.Product,
		ProductVersion: coreVersion,
		Artifact:       artifact,
		JavaAdvisory:   21,
	}, nil
}

// TestInitializeThenLockedInstallReplaysInEmptyWorkspace chains adoption and
// installation in process: initialization detects a synthetic vanilla runtime,
// writes its manifest and lock, and a locked install in an empty destination
// replays those documents without resolving or rewriting them.
func TestInitializeThenLockedInstallReplaysInEmptyWorkspace(t *testing.T) {
	isolateOperationHome(t)
	core := []byte("initialized core bytes\n")
	source := &replaySource{core: catalog.Artifact{
		Provider:  catalog.ProviderMojang,
		ProjectID: "minecraft",
		Version:   "1.21.4",
		Filename:  "server.jar",
		URL:       "https://example.invalid/server.jar",
		Hashes:    map[string]string{"sha256": operationTestDigest(core)},
	}}
	tracker := filetrack.NewService(&http.Client{}, nil)
	installer := install.NewService(source, nil, tracker, nil, nil, "")
	service := NewService(installer, source, nil, nil)

	sourceRoot := t.TempDir()
	operationTestWrite(t, sourceRoot, "server.jar", operationTestMiniVanillaJar(t, "1.21.4"), 0o644)
	if _, err := service.Initialize(t.Context(), sourceRoot, InitOptions{}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	manifestBytes := operationTestRead(t, sourceRoot, document.ManifestFilename)
	lockBytes := operationTestRead(t, sourceRoot, document.LockFilename)
	if _, err := os.Stat(filepath.Join(sourceRoot, ".lucy", "install.yaml")); !os.IsNotExist(err) {
		t.Fatalf("init staged an installation receipt: %v", err)
	}
	manifest, err := document.ManifestFile(sourceRoot).Read()
	if err != nil {
		t.Fatalf("read initialized manifest: %v", err)
	}
	lock, err := document.LockFile(sourceRoot).Read()
	if err != nil {
		t.Fatalf("read initialized lock: %v", err)
	}
	if manifest.Server.Core.Product != "minecraft" ||
		manifest.Server.Core.ProductVersion != "1.21.4" ||
		manifest.Server.Minecraft != "1.21.4" ||
		manifest.Server.Directory != "." {
		t.Fatalf("initialized identity = %+v", manifest.Server)
	}
	if lock.Server.Product != "minecraft" || lock.Server.CoreVersion != "1.21.4" ||
		lock.Server.Directory != "." {
		t.Fatalf("initialized lock identity = %+v", lock.Server)
	}

	destination := t.TempDir()
	operationTestWrite(t, destination, document.ManifestFilename, manifestBytes, 0o644)
	operationTestWrite(t, destination, document.LockFilename, lockBytes, 0o644)
	materialize := operationTestMaterializer(t, map[string][]byte{"server.jar": core})
	options := install.Options{Locked: true, Offline: true, Materialize: materialize}
	result, err := installer.Sync(t.Context(), destination, manifest, lock, options)
	if err != nil {
		t.Fatalf("install initialized lock: %v", err)
	}
	if result.Mode != install.ModeReplay {
		t.Fatalf("install mode = %q, want %q", result.Mode, install.ModeReplay)
	}
	if !bytes.Equal(operationTestRead(t, destination, document.ManifestFilename), manifestBytes) {
		t.Fatal("install rewrote the manifest that init generated")
	}
	if !bytes.Equal(operationTestRead(t, destination, document.LockFilename), lockBytes) {
		t.Fatal("install resolved again instead of replaying the initialized lock")
	}
	if actual := operationTestRead(t, destination, "server.jar"); !bytes.Equal(actual, core) {
		t.Fatalf("installed core = %q, want %q", actual, core)
	}
	if _, err := os.Stat(filepath.Join(destination, ".lucy", "install.yaml")); err != nil {
		t.Fatalf("installation receipt: %v", err)
	}

	coreInfo, err := os.Stat(filepath.Join(destination, "server.jar"))
	if err != nil {
		t.Fatalf("stat installed core: %v", err)
	}
	again, err := installer.Sync(t.Context(), destination, manifest, lock, options)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if again.Files.Created != 0 || again.Files.Replaced != 0 {
		t.Fatalf("a current installation reported changes: %+v", again)
	}
	info, err := os.Stat(filepath.Join(destination, "server.jar"))
	if err != nil {
		t.Fatalf("stat replayed core: %v", err)
	}
	if !info.ModTime().Equal(coreInfo.ModTime()) {
		t.Fatal("a no-op replay touched the installed core")
	}
	if !bytes.Equal(operationTestRead(t, destination, document.ManifestFilename), manifestBytes) ||
		!bytes.Equal(operationTestRead(t, destination, document.LockFilename), lockBytes) {
		t.Fatal("a no-op replay rewrote the initialized documents")
	}
}
