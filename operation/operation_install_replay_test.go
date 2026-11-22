package operation

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
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
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/receipt"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/serverregistry"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

func isolateOperationHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "appdata"))
}

func operationTestRegistry(t *testing.T) *serverregistry.Registry {
	t.Helper()
	registry, err := serverregistry.New()
	if err != nil {
		t.Fatalf("server registry: %v", err)
	}
	return registry
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

func (s *replaySource) GetArtifactReleaseMapper(catalog.Provider) (upstream.ArtifactReleaseMapper, bool, error) {
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

func (s *replaySource) ResolveArtifactsByHash(
	_ context.Context,
	artifacts []upstream.ArtifactFingerprint,
) []upstream.ArtifactReleaseResolution {
	return make([]upstream.ArtifactReleaseResolution, len(artifacts))
}

func (s *replaySource) Metadata(_ context.Context, endpoint string, _ any) error {
	return fmt.Errorf("unexpected metadata request for %s", endpoint)
}

func (s *replaySource) ResolveServerProduct(
	_ context.Context,
	request upstream.ServerProductRequest,
	_ func(*catalog.Artifact) (artifact.MaterializedFile, error),
) (upstream.ServerProductResolution, error) {
	coreVersion := request.ProductVersion
	if coreVersion == "" || coreVersion.CanInfer() || coreVersion.IsInvalid() {
		coreVersion = "1.21.4"
	}
	artifact := s.core
	artifact.Version = coreVersion
	return upstream.ServerProductResolution{
		Minecraft:      request.Minecraft,
		Product:        request.Product,
		ProductVersion: coreVersion,
		Artifact:       artifact,
		Bootstrap:      serverpkg.BootstrapMethodVanilla,
		JavaAdvisory:   21,
	}, nil
}

func TestInitializeThenLockedInstallReplaysInEmptyWorkspace(t *testing.T) {
	t.Run("fresh resolution", func(t *testing.T) {
		testInitializedLockReplaysOffline(t, false)
	})
	t.Run("current SHA-1 lock", func(t *testing.T) {
		testInitializedLockReplaysOffline(t, true)
	})
}

func testInitializedLockReplaysOffline(t *testing.T, reuseLock bool) {
	t.Helper()
	isolateOperationHome(t)
	core := operationTestMiniVanillaJar(t, "1.21.4")
	coreArtifact := catalog.Artifact{
		Provider:  catalog.ProviderMojang,
		ProjectID: "minecraft",
		Version:   "1.21.4",
		Filename:  "server.jar",
		URL:       "https://example.invalid/server.jar",
		Hashes:    map[string]string{"sha256": operationTestDigest(core)},
	}
	source := &replaySource{}
	if !reuseLock {
		source.core = coreArtifact
	}
	tracker := filetrack.NewService(&http.Client{}, nil)
	registry := operationTestRegistry(t)
	installer := install.NewService(source, nil, tracker, nil, registry, nil, "", fsutil.LinkClone)
	service := NewService(installer, source, registry, nil)

	sourceRoot := t.TempDir()
	operationTestWrite(t, sourceRoot, "server.jar", core, 0o644)
	opts := InitOptions{}
	if reuseLock {
		manifest := &document.Manifest{
			SchemaVersion: document.ManifestSchemaVersion,
			Server: document.ManifestServer{
				Minecraft: "1.21.4",
				Directory: ".",
				Core: document.ManifestCore{
					Product:        serverpkg.ProductMinecraft,
					ProductVersion: "1.21.4",
				},
			},
		}
		if err := document.ManifestFile(sourceRoot).Write(manifest); err != nil {
			t.Fatalf("write existing manifest: %v", err)
		}
		fingerprint, err := manifest.Fingerprint()
		if err != nil {
			t.Fatal(err)
		}
		coreArtifact.Hashes = map[string]string{"sha1": fmt.Sprintf("%x", sha1.Sum(core))}
		lock := &document.Lock{
			SchemaVersion:       document.LockSchemaVersion,
			ManifestFingerprint: fingerprint,
			Server: document.LockedServer{
				Minecraft:   "1.21.4",
				Directory:   ".",
				Product:     serverpkg.ProductMinecraft,
				CoreVersion: "1.21.4",
				Artifact:    coreArtifact,
				Bootstrap:   serverpkg.BootstrapMethodVanilla,
				Java:        document.LockedJava{Advisory: 21},
			},
		}
		if err := document.LockFile(sourceRoot).Write(lock); err != nil {
			t.Fatalf("write existing lock: %v", err)
		}
		opts.Force = true
	}
	if _, err := service.Initialize(t.Context(), sourceRoot, opts); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	manifestBytes := operationTestRead(t, sourceRoot, document.ManifestFilename)
	lockBytes := operationTestRead(t, sourceRoot, document.LockFilename)
	adopted, err := receipt.Read(sourceRoot)
	if err != nil {
		t.Fatalf("read adopted receipt: %v", err)
	}
	if adopted.BootstrapFingerprint == "" {
		t.Fatal("initialization did not record the adopted bootstrap identity")
	}
	if len(adopted.Entries) != 1 || adopted.Entries[0].Path != "server.jar" ||
		adopted.Entries[0].Origin != receipt.OriginBootstrap ||
		adopted.Entries[0].SHA256 != operationTestDigest(core) {
		t.Fatalf("initialization did not record the adopted runtime: %+v", adopted.Entries)
	}
	manifest, err := document.ManifestFile(sourceRoot).Read()
	if err != nil {
		t.Fatalf("read initialized manifest: %v", err)
	}
	lock, err := document.LockFile(sourceRoot).Read()
	if err != nil {
		t.Fatalf("read initialized lock: %v", err)
	}
	if reuseLock && lock.Server.Hashes["sha1"] != coreArtifact.Hashes["sha1"] {
		t.Fatalf("initialization changed the original SHA-1 pin: %v", lock.Server.Hashes)
	}

	destination := t.TempDir()
	operationTestWrite(t, destination, document.ManifestFilename, manifestBytes, 0o644)
	operationTestWrite(t, destination, document.LockFilename, lockBytes, 0o644)
	options := install.Options{Locked: true, Offline: true}
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
}
