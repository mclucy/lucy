package install_test

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
	"runtime"
	"testing"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/install"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/serverregistry"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	versionpkg "github.com/mclucy/lucy/version"
	"gopkg.in/yaml.v3"
)

// isolateInstallHome keeps Java selection and configuration reads inside the
// test process, so a developer's machine configuration cannot decide an
// in-process installation outcome.
func isolateInstallHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "appdata"))
}

func installTestRegistry(t *testing.T) *serverregistry.Registry {
	t.Helper()
	registry, err := serverregistry.New()
	if err != nil {
		t.Fatalf("server registry: %v", err)
	}
	return registry
}

func installTestWrite(t *testing.T, root, relative string, data []byte, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", relative, err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

func installTestRead(t *testing.T, root, relative string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return data
}

func installTestDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// installTestMaterializer writes injected bytes to a private directory and
// returns their path. Installation recomputes the digests, so the mapping must
// name every artifact the pipeline asks for.
func installTestMaterializer(t *testing.T, files map[string][]byte) func(*catalog.Artifact) (artifact.MaterializedFile, error) {
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
			Hashes: map[string]string{"sha256": installTestDigest(data)},
		}, nil
	}
}

func installTestService(t *testing.T) *install.Service {
	t.Helper()
	return install.NewService(
		nil,
		nil,
		filetrack.NewService(&http.Client{}, nil),
		nil,
		installTestRegistry(t),
		nil,
		"",
	)
}

func installTestReceipt(t *testing.T, root string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".lucy", "install.yaml"))
	if err != nil {
		t.Fatalf("read installation receipt: %v", err)
	}
	var receipt struct {
		Version int `yaml:"version"`
		Entries []struct {
			Path   string `yaml:"path"`
			SHA256 string `yaml:"sha256"`
			Mode   uint32 `yaml:"mode"`
		} `yaml:"entries"`
	}
	if err := yaml.Unmarshal(raw, &receipt); err != nil {
		t.Fatalf("decode installation receipt: %v", err)
	}
	entries := make(map[string]string, len(receipt.Entries))
	for _, entry := range receipt.Entries {
		entries[entry.Path] = entry.SHA256
	}
	return entries
}

func installTestManifest(coreVersion string) *document.Manifest {
	return &document.Manifest{
		SchemaVersion: document.ManifestSchemaVersion,
		Name:          "test",
		Server: document.ManifestServer{
			Minecraft: "1.21.4",
			Directory: "server",
			Core: document.ManifestCore{
				Product:        serverpkg.ProductMinecraft,
				ProductVersion: versionpkg.Version(coreVersion),
			},
			Packages: map[catalog.Ecosystem]map[string]document.Requirement{},
		},
	}
}

// pipelineSource supplies the single server artifact the resolution test
// installs. Catalogue packages stay empty so the test exercises the
// source-driven pipeline without a provider or network.
type pipelineSource struct {
	core catalog.Artifact
}

func (s *pipelineSource) Candidates(
	context.Context,
	catalog.Registry,
	string,
	string,
	catalog.Ecosystem,
) ([]upstream.Candidate, error) {
	return nil, nil
}

func (s *pipelineSource) Metadata(context.Context, string, any) error {
	return fmt.Errorf("unexpected metadata request")
}

func (s *pipelineSource) ResolveServerProduct(
	_ context.Context,
	request routing.ServerProductRequest,
	_ func(*catalog.Artifact) (artifact.MaterializedFile, error),
) (routing.ServerProductResolution, error) {
	core := s.core
	core.Version = request.ProductVersion
	return routing.ServerProductResolution{
		Minecraft:      request.Minecraft,
		Product:        request.Product,
		ProductVersion: request.ProductVersion,
		Artifact:       core,
		JavaAdvisory:   21,
	}, nil
}

func TestResolveInstallUsesInjectedSourceAndArtifacts(t *testing.T) {
	isolateInstallHome(t)
	root := t.TempDir()
	core := []byte("resolved core bytes\n")
	manifest := installTestManifest("1.21.4")
	if err := document.ManifestFile(root).Write(manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manifestBytes := installTestRead(t, root, document.ManifestFilename)
	source := &pipelineSource{core: catalog.Artifact{
		Provider:  catalog.ProviderMojang,
		ProjectID: "minecraft",
		Version:   "1.21.4",
		Filename:  "server.jar",
		URL:       "https://example.invalid/server.jar",
		Hashes:    map[string]string{"sha256": installTestDigest(core)},
	}}
	tracker := filetrack.NewService(&http.Client{}, nil)
	service := install.NewService(source, nil, tracker, nil, installTestRegistry(t), nil, "")
	materialize := installTestMaterializer(t, map[string][]byte{"server.jar": core})
	result, err := service.Sync(t.Context(), root, manifest, nil, install.Options{Materialize: materialize})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Mode != install.ModeResolve {
		t.Fatalf("Sync() mode = %q, want %q", result.Mode, install.ModeResolve)
	}
	if actual := installTestRead(t, root, "server/server.jar"); !bytes.Equal(actual, core) {
		t.Fatalf("server core = %q, want %q", actual, core)
	}
	lock, err := document.LockFile(root).Read()
	if err != nil {
		t.Fatalf("read resolved lock: %v", err)
	}
	if lock.Server.Product != serverpkg.ProductMinecraft || lock.Server.CoreVersion != "1.21.4" ||
		lock.Server.Artifact.Hashes["sha256"] != installTestDigest(core) {
		t.Fatalf("resolved lock = %+v", lock.Server)
	}
	entries := installTestReceipt(t, root)
	if entries["server/server.jar"] != installTestDigest(core) {
		t.Fatalf("receipt core entry = %q, want %q", entries["server/server.jar"], installTestDigest(core))
	}
	if !bytes.Equal(installTestRead(t, root, document.ManifestFilename), manifestBytes) {
		t.Fatal("resolution rewrote the manifest it did not publish")
	}
}

func TestLockedReplayInstallsEmptyWorkspaceWithoutRewritingIntent(t *testing.T) {
	isolateInstallHome(t)
	root := t.TempDir()
	core := []byte("replay core bytes\n")
	pkg := []byte("replay package bytes\n")

	manifest := installTestManifest("1.21.4")
	manifest.Server.Packages[catalog.EcosystemFabric] = map[string]document.Requirement{
		"modrinth:alpha": {Selector: "1.0.0"},
	}
	if err := document.ManifestFile(root).Write(manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	fingerprint, err := manifest.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint manifest: %v", err)
	}
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: fingerprint,
		Server: document.LockedServer{
			Minecraft:   "1.21.4",
			Directory:   "server",
			Product:     serverpkg.ProductMinecraft,
			CoreVersion: "1.21.4",
			Artifact: catalog.Artifact{
				Provider: catalog.ProviderMojang,
				Version:  "1.21.4",
				Filename: "server.jar",
				URL:      "https://example.invalid/server.jar",
				Hashes:   map[string]string{"sha256": installTestDigest(core)},
			},
			Java: document.LockedJava{Advisory: 21},
		},
		Packages: []document.Package{{
			Environment: serverpkg.EnvironmentServer,
			Ecosystem:   catalog.EcosystemFabric,
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderModrinth,
				ProjectID: "alpha",
				Version:   "1.0.0",
				Filename:  "alpha.jar",
				URL:       "https://example.invalid/alpha.jar",
				Hashes:    map[string]string{"sha256": installTestDigest(pkg)},
			},
			Requirements: []document.RequirementBinding{{
				Reference: "modrinth:alpha",
				Selector:  "1.0.0",
				Enabled:   true,
			}},
		}},
	}
	if err := document.LockFile(root).Write(lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	manifestBytes := installTestRead(t, root, document.ManifestFilename)
	lockBytes := installTestRead(t, root, document.LockFilename)

	service := installTestService(t)
	materialize := installTestMaterializer(t, map[string][]byte{
		"server.jar": core,
		"alpha.jar":  pkg,
	})
	options := install.Options{Locked: true, Offline: true, Materialize: materialize}
	result, err := service.Sync(t.Context(), root, manifest, lock, options)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Mode != install.ModeReplay {
		t.Fatalf("Sync() mode = %q, want %q", result.Mode, install.ModeReplay)
	}
	if result.PublishedLock || result.PublishedManifest {
		t.Fatalf("replay published documents: %+v", result)
	}
	if actual := installTestRead(t, root, "server/server.jar"); !bytes.Equal(actual, core) {
		t.Fatalf("server core = %q, want %q", actual, core)
	}
	if actual := installTestRead(t, root, "server/mods/alpha.jar"); !bytes.Equal(actual, pkg) {
		t.Fatalf("package = %q, want %q", actual, pkg)
	}
	entries := installTestReceipt(t, root)
	if entries["server/server.jar"] != installTestDigest(core) {
		t.Fatalf("receipt core entry = %q, want %q", entries["server/server.jar"], installTestDigest(core))
	}
	if entries["server/mods/alpha.jar"] != installTestDigest(pkg) {
		t.Fatalf("receipt package entry = %q, want %q", entries["server/mods/alpha.jar"], installTestDigest(pkg))
	}
	if !bytes.Equal(installTestRead(t, root, document.ManifestFilename), manifestBytes) {
		t.Fatal("install rewrote the manifest that drove the replay")
	}
	if !bytes.Equal(installTestRead(t, root, document.LockFilename), lockBytes) {
		t.Fatal("install resolved again instead of replaying the lock")
	}

	coreInfo, err := os.Stat(filepath.Join(root, "server", "server.jar"))
	if err != nil {
		t.Fatalf("stat installed core: %v", err)
	}
	again, err := service.Sync(t.Context(), root, manifest, lock, options)
	if err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}
	if again.Mode != install.ModeReplay || again.Files.Created != 0 || again.Files.Replaced != 0 {
		t.Fatalf("a current installation reported changes: %+v", again)
	}
	info, err := os.Stat(filepath.Join(root, "server", "server.jar"))
	if err != nil {
		t.Fatalf("stat installed core: %v", err)
	}
	if !info.ModTime().Equal(coreInfo.ModTime()) {
		t.Fatal("a no-op replay touched the installed core")
	}
	if !bytes.Equal(installTestRead(t, root, document.ManifestFilename), manifestBytes) {
		t.Fatal("a no-op replay rewrote the manifest")
	}
	if !bytes.Equal(installTestRead(t, root, document.LockFilename), lockBytes) {
		t.Fatal("a no-op replay rewrote the lock")
	}
}

func TestAdoptionReplacesOwnedLauncherAndPreservesAmbientFiles(t *testing.T) {
	isolateInstallHome(t)
	root := t.TempDir()

	core := []byte("fabric loader core bytes\n")
	serverInput := []byte("new server input bytes\n")
	ambient := []byte("ambient package bytes\n")
	extra := []byte("unmanaged note\n")
	adoptedShim := installTestFabricShim(t)
	adoptedServerJar := installTestMiniVanillaJar(t, "1.21.4")

	installTestWrite(t, root, "server/fabric-server-launch.jar", adoptedShim, 0o644)
	installTestWrite(t, root, "server/fabric-server-launcher.properties", []byte("serverJar=server.jar\n"), 0o644)
	installTestWrite(t, root, "server/server.jar", adoptedServerJar, 0o644)
	installTestWrite(t, root, "server/mods/ambient.jar", ambient, 0o644)
	installTestWrite(t, root, "server/mods/extra.jar", extra, 0o644)

	manifest := installTestManifest("0.16.9")
	manifest.Server.Core.Product = serverpkg.ProductFabric
	manifest.Server.Packages[catalog.EcosystemFabric] = map[string]document.Requirement{
		"modrinth:ambient": {Selector: "1.0.0"},
	}
	if err := document.ManifestFile(root).Write(manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	fingerprint, err := manifest.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint manifest: %v", err)
	}
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: fingerprint,
		Server: document.LockedServer{
			Minecraft:   "1.21.4",
			Directory:   "server",
			Product:     serverpkg.ProductFabric,
			CoreVersion: "0.16.9",
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderFabric,
				ProjectID: "fabric-loader",
				Version:   "0.16.9",
				Filename:  "fabric-loader-0.16.9.jar",
				URL:       "https://example.invalid/fabric-loader-0.16.9.jar",
				Hashes:    map[string]string{"sha256": installTestDigest(core)},
			},
			Bootstrapper: "fabric",
			Inputs: []serverpkg.BootstrapInput{{
				Path: "server.jar",
				Artifact: catalog.Artifact{
					Provider: catalog.ProviderMojang,
					Version:  "1.21.4",
					Filename: "server.jar",
					URL:      "https://example.invalid/server.jar",
					Hashes:   map[string]string{"sha256": installTestDigest(serverInput)},
				},
			}},
			Java: document.LockedJava{Advisory: 21},
		},
		Packages: []document.Package{{
			Environment: serverpkg.EnvironmentServer,
			Ecosystem:   catalog.EcosystemFabric,
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderModrinth,
				ProjectID: "ambient",
				Version:   "1.0.0",
				Filename:  "ambient.jar",
				URL:       "https://example.invalid/ambient.jar",
				Hashes:    map[string]string{"sha256": installTestDigest(ambient)},
			},
			Requirements: []document.RequirementBinding{{
				Reference: "modrinth:ambient",
				Selector:  "1.0.0",
				Enabled:   true,
			}},
		}},
	}
	if err := document.LockFile(root).Write(lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	manifestBytes := installTestRead(t, root, document.ManifestFilename)
	lockBytes := installTestRead(t, root, document.LockFilename)

	service := installTestService(t)
	materialize := installTestMaterializer(t, map[string][]byte{
		"fabric-loader-0.16.9.jar": core,
		"server.jar":               serverInput,
		"ambient.jar":              ambient,
	})
	options := install.Options{Locked: true, Offline: true, Materialize: materialize}
	result, err := service.Sync(t.Context(), root, manifest, lock, options)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Mode != install.ModeReplay {
		t.Fatalf("Sync() mode = %q, want %q", result.Mode, install.ModeReplay)
	}

	launcher := installTestRead(t, root, "server/fabric-server-launch.jar")
	if bytes.Equal(launcher, adoptedShim) || bytes.Contains(launcher, []byte("written by the upstream installer")) {
		t.Fatal("install kept the adopted launcher instead of generating its own")
	}
	if !bytes.Equal(installTestRead(t, root, "server/server.jar"), serverInput) {
		t.Fatal("install did not replace the adopted payload it owns")
	}
	if !bytes.Equal(installTestRead(t, root, "server/mods/ambient.jar"), ambient) {
		t.Fatal("install rewrote an adopted package it locked")
	}
	if !bytes.Equal(installTestRead(t, root, "server/mods/extra.jar"), extra) {
		t.Fatal("install pruned an ambient package it never owned")
	}
	run, err := os.Stat(filepath.Join(root, "server", "run.sh"))
	if err != nil {
		t.Fatalf("stat generated launcher script: %v", err)
	}
	if runtime.GOOS != "windows" && run.Mode().Perm()&0o111 == 0 {
		t.Fatalf("generated launcher %s is not executable", run.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(root, "server", "libraries/net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar")); err != nil {
		t.Fatalf("generated loader library: %v", err)
	}
	entries := installTestReceipt(t, root)
	for path, want := range map[string]string{
		"server/server.jar":               installTestDigest(serverInput),
		"server/fabric-server-launch.jar": installTestDigest(launcher),
		"server/mods/ambient.jar":         installTestDigest(ambient),
	} {
		if entries[path] != want {
			t.Fatalf("receipt entry %s = %q, want %q", path, entries[path], want)
		}
	}
	if _, owned := entries["server/mods/extra.jar"]; owned {
		t.Fatal("receipt claimed an ambient package the installation never owned")
	}
	if !bytes.Equal(installTestRead(t, root, document.ManifestFilename), manifestBytes) {
		t.Fatal("adoption install rewrote the manifest")
	}
	if !bytes.Equal(installTestRead(t, root, document.LockFilename), lockBytes) {
		t.Fatal("adoption install rewrote the lock")
	}

	launcherInfo, err := os.Stat(filepath.Join(root, "server", "fabric-server-launch.jar"))
	if err != nil {
		t.Fatalf("stat generated launcher: %v", err)
	}
	again, err := service.Sync(t.Context(), root, manifest, lock, options)
	if err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}
	if again.Files.Created != 0 || again.Files.Replaced != 0 {
		t.Fatalf("reinstall adopted workspace reported changes: %+v", again)
	}
	current, err := os.Stat(filepath.Join(root, "server", "fabric-server-launch.jar"))
	if err != nil {
		t.Fatalf("stat regenerated launcher: %v", err)
	}
	if !current.ModTime().Equal(launcherInfo.ModTime()) {
		t.Fatal("reinstall regenerated the launcher it already owns")
	}
	if !bytes.Equal(installTestRead(t, root, "server/mods/extra.jar"), extra) {
		t.Fatal("reinstall removed the ambient package")
	}
}

func installTestMiniVanillaJar(t *testing.T, id string) []byte {
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

// installTestFabricShim builds an upstream-style launch shim whose identity
// discovery can read. The marker in its embedded properties keeps it distinct
// from every launcher Lucy generates.
func installTestFabricShim(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	manifest, err := writer.Create("META-INF/MANIFEST.MF")
	if err != nil {
		t.Fatalf("create shim manifest: %v", err)
	}
	_, err = fmt.Fprint(
		manifest,
		"Manifest-Version: 1.0\nClass-Path: libraries/net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar libraries/net/fabricmc/intermediary/1.21.4/intermediary-1.21.4.jar\n",
	)
	if err != nil {
		t.Fatalf("write shim manifest: %v", err)
	}
	properties, err := writer.Create("fabric-server-launch.properties")
	if err != nil {
		t.Fatalf("create shim properties: %v", err)
	}
	_, err = fmt.Fprint(
		properties,
		"launch.mainClass=net.fabricmc.loader.impl.launch.knot.KnotServer\n# written by the upstream installer\n",
	)
	if err != nil {
		t.Fatalf("write shim properties: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("seal shim: %v", err)
	}
	return buffer.Bytes()
}
