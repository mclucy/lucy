package operation

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	logpkg "github.com/mclucy/lucy/log"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
	"go.yaml.in/yaml/v4"
)

// initializeTestSource answers the workspace-observation capabilities
// initialization consumes. Synthetic layouts carry no upstream identity, so
// every lookup stays empty and no network is involved.
type initializeTestSource struct{}

func (initializeTestSource) Candidates(
	context.Context,
	catalog.Registry,
	string,
	string,
	catalog.Ecosystem,
) ([]upstream.Candidate, error) {
	return nil, nil
}

func (initializeTestSource) GetArtifactMapper(
	catalog.Provider,
) (upstream.ArtifactMapProvider, bool, error) {
	return nil, false, nil
}

func (initializeTestSource) IdentifyFillBuild(
	context.Context,
	string,
	versionpkg.Version,
	string,
) (versionpkg.Version, bool, error) {
	return versionpkg.Unknown, false, nil
}

func (initializeTestSource) IdentifyPurpurBuild(
	context.Context,
	versionpkg.Version,
	string,
) (versionpkg.Version, bool, error) {
	return versionpkg.Unknown, false, nil
}

func (initializeTestSource) ResolveArtifactByHash(
	context.Context,
	upstream.ArtifactFingerprint,
) upstream.ArtifactLookup {
	return upstream.ArtifactLookup{}
}

// initializeInstallSource resolves the requested runtime into a lock record
// without transferring bytes. Fabric core modules are supplied directly so
// resolution never asks for the artifact.
type initializeInstallSource struct{}

func (initializeInstallSource) Candidates(
	context.Context,
	catalog.Registry,
	string,
	string,
	catalog.Ecosystem,
) ([]upstream.Candidate, error) {
	return nil, nil
}

func (initializeInstallSource) Metadata(context.Context, string, any) error {
	return nil
}

func (initializeInstallSource) ResolveServerProduct(
	_ context.Context,
	request upstream.ServerProductRequest,
	_ func(*catalog.Artifact) (artifact.MaterializedFile, error),
) (upstream.ServerProductResolution, error) {
	resolution := upstream.ServerProductResolution{
		Minecraft:      request.Minecraft,
		Product:        request.Product,
		ProductVersion: request.ProductVersion,
		Artifact: catalog.Artifact{
			Version:  request.ProductVersion,
			Filename: request.Product.String() + "-" + request.ProductVersion.String() + ".jar",
		},
		JavaAdvisory: 21,
	}
	if request.Product == serverpkg.ProductFabric {
		resolution.Artifact.Modules = []catalog.DescriptorModule{{
			ID:        "fabric-loader",
			Version:   request.ProductVersion,
			Ecosystem: catalog.EcosystemFabric,
		}}
	}
	return resolution, nil
}

// newInitializeTestService builds the operation service with the default
// source, which identifies no package.
func newInitializeTestService(t *testing.T) *Service {
	t.Helper()
	return newInitializeTestServiceWithSource(t, initializeTestSource{})
}

// newInitializeTestServiceWithSource builds the operation service over an
// isolated home: initialization acquires the per-user artifact store and
// registers the workspace, so neither may reach the developer's real data
// directory.
func newInitializeTestServiceWithSource(t *testing.T, source Source) *Service {
	t.Helper()
	isolateOperationHome(t)
	registry := operationTestRegistry(t)
	logger, err := logpkg.New(filepath.Join(t.TempDir(), "lucy.log"), "debug", nil)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Errorf("close logger: %v", err)
		}
	})
	installer := install.NewService(
		initializeInstallSource{},
		nil,
		nil,
		nil,
		registry,
		logger,
		"",
	)
	return NewService(
		installer,
		source,
		registry,
		logger,
	)
}

func writeInitializeTestJar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir jar directory: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jar %s: %v", path, err)
	}
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create jar entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write jar entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close jar writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close jar file: %v", err)
	}
}

func writeVanillaTestJar(t *testing.T, root, name, minecraft string) string {
	t.Helper()
	path := filepath.Join(root, name)
	writeInitializeTestJar(t, path, map[string]string{
		"version.json": `{"id":"` + minecraft + `","name":"` + minecraft + `"}`,
	})
	return path
}

func writeFabricTestLauncher(t *testing.T, root, loader, minecraft string) string {
	t.Helper()
	path := filepath.Join(
		root,
		"fabric-server-mc."+minecraft+"-loader."+loader+"-launcher.1.1.2.jar",
	)
	writeInitializeTestJar(t, path, map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nMain-Class: net.fabricmc.installer.ServerLauncher\n\n",
		"install.properties":   "fabric-loader-version=" + loader + "\ngame-version=" + minecraft + "\n",
	})
	return path
}

func readManifest(t *testing.T, root string) *documentpkg.Manifest {
	t.Helper()
	manifest, err := documentpkg.ManifestFile(root).Read()
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	return manifest
}

func readLock(t *testing.T, root string) *documentpkg.Lock {
	t.Helper()
	lock, err := documentpkg.LockFile(root).Read()
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	return lock
}

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// Initialization derives the server declaration and lock from the directory
// alone, and never stages an installation receipt.
func TestInitializeWritesDeclarationForVanillaLayout(t *testing.T) {
	root := t.TempDir()
	writeVanillaTestJar(t, root, "server.jar", "1.20.1")

	result, err := newInitializeTestService(t).Initialize(
		t.Context(),
		root,
		InitOptions{},
	)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	manifest := readManifest(t, root)
	if got := manifest.Server.Minecraft.String(); got != "1.20.1" {
		t.Fatalf("manifest Minecraft = %q, want 1.20.1", got)
	}
	if got := manifest.Server.Core.Product; got != serverpkg.ProductMinecraft {
		t.Fatalf("manifest product = %q, want minecraft", got)
	}
	if got := manifest.Server.Core.ProductVersion.String(); got != "1.20.1" {
		t.Fatalf("manifest core version = %q, want 1.20.1", got)
	}
	if got := manifest.Server.Directory; got != "." {
		t.Fatalf("manifest server directory = %q, want \".\"", got)
	}

	lock := readLock(t, root)
	if got := lock.Server.Product; got != serverpkg.ProductMinecraft {
		t.Fatalf("lock product = %q, want minecraft", got)
	}
	if got := lock.Server.Minecraft.String(); got != "1.20.1" {
		t.Fatalf("lock Minecraft = %q, want 1.20.1", got)
	}
	if got := lock.Server.CoreVersion.String(); got != "1.20.1" {
		t.Fatalf("lock core version = %q, want 1.20.1", got)
	}
	fingerprint, err := manifest.Fingerprint()
	if err != nil {
		t.Fatalf("manifest fingerprint: %v", err)
	}
	if lock.ManifestFingerprint != fingerprint {
		t.Fatalf(
			"lock fingerprint = %q, want the written manifest fingerprint %q",
			lock.ManifestFingerprint,
			fingerprint,
		)
	}
	if !result.PublishedLock || !result.PublishedConfig {
		t.Fatalf(
			"published lock/config = %t/%t, want both published",
			result.PublishedLock,
			result.PublishedConfig,
		)
	}

	// The lock records the runtime identity, never an ephemeral selection.
	data := readFileBytes(t, filepath.Join(root, documentpkg.LockFilename))
	var decoded map[string]any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode lock YAML: %v", err)
	}
	server, _ := decoded["server"].(map[string]any)
	if _, ok := server["entry_point"]; ok {
		t.Fatalf("lock records an entry point:\n%s", data)
	}
}

// Initialization reads the workspace and writes declarations; the files it
// observed, including an existing receipt, stay byte-for-byte intact.
func TestInitializePreservesReceiptAndObservedFiles(t *testing.T) {
	root := t.TempDir()
	launcherPath := writeFabricTestLauncher(t, root, "0.19.5", "1.21.1")
	modPath := filepath.Join(root, "mods", "example.jar")
	writeInitializeTestJar(t, modPath, map[string]string{
		"fabric.mod.json": `{"schemaVersion":1,"id":"example","version":"1.0.0"}`,
	})
	receiptPath := filepath.Join(root, ".lucy", "install.yaml")
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o755); err != nil {
		t.Fatalf("mkdir receipt directory: %v", err)
	}
	modSum := sha256.Sum256(readFileBytes(t, modPath))
	receipt := fmt.Appendf(
		nil,
		"version: 1\nentries:\n  - path: mods/example.jar\n    sha256: %x\n    origin: package\n    package: modrinth:example\n",
		modSum,
	)
	if err := os.WriteFile(receiptPath, receipt, 0o644); err != nil {
		t.Fatalf("write receipt: %v", err)
	}
	launcher := readFileBytes(t, launcherPath)
	mod := readFileBytes(t, modPath)

	result, err := newInitializeTestService(t).Initialize(
		t.Context(),
		root,
		InitOptions{},
	)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if got := readFileBytes(t, receiptPath); string(got) != string(receipt) {
		t.Fatalf("receipt changed during initialization:\n%s", got)
	}
	if got := readFileBytes(t, launcherPath); string(got) != string(launcher) {
		t.Fatalf("server launcher changed during initialization")
	}
	if got := readFileBytes(t, modPath); string(got) != string(mod) {
		t.Fatalf("installed package changed during initialization")
	}
	if got := result.Document.Server.Core.Product; got != serverpkg.ProductFabric {
		t.Fatalf("documented product = %q, want fabric", got)
	}
	if got := result.Document.Server.Core.ProductVersion.String(); got != "0.19.5" {
		t.Fatalf("documented core version = %q, want 0.19.5", got)
	}
}

// Competing runtimes are a refusal, not a guess. The exact selection is
// ephemeral and never leaks into the lock, while later observation keeps the
// identity the initialization wrote.
func TestInitializeRequiresSelectionForCompetingRuntimes(t *testing.T) {
	root := t.TempDir()
	writeFabricTestLauncher(t, root, "0.19.5", "1.21.1")
	writeVanillaTestJar(t, root, "server.jar", "1.21.1")
	service := newInitializeTestService(t)

	result, err := service.Initialize(t.Context(), root, InitOptions{})
	var selectionErr *discovery.SelectionError
	if !errors.As(err, &selectionErr) {
		t.Fatalf("Initialize error = %v, want *discovery.SelectionError", err)
	}
	if selectionErr.State != discovery.SelectionAmbiguous {
		t.Fatalf("selection state = %q, want ambiguous", selectionErr.State)
	}
	if len(selectionErr.Installations) != 2 {
		t.Fatalf(
			"ambiguous candidates = %d, want 2",
			len(selectionErr.Installations),
		)
	}
	if result == nil || result.Document != nil {
		t.Fatalf("refused initialization must not report a written document")
	}
	if _, err := os.Stat(filepath.Join(root, documentpkg.ManifestFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused initialization wrote a manifest: %v", err)
	}

	if _, err := service.Initialize(
		t.Context(),
		root,
		InitOptions{Expectation: &discovery.Expectation{
			Product: serverpkg.ProductFabric,
		}},
	); err != nil {
		t.Fatalf("Initialize with selected core: %v", err)
	}
	manifest := readManifest(t, root)
	if got := manifest.Server.Core.Product; got != serverpkg.ProductFabric {
		t.Fatalf("manifest product = %q, want fabric", got)
	}
	lock := readLock(t, root)
	if lock.Server.Product != serverpkg.ProductFabric ||
		lock.Server.CoreVersion.String() != "0.19.5" ||
		lock.Server.Minecraft.String() != "1.21.1" {
		t.Fatalf(
			"lock identity = %s/%s@%s, want fabric/1.21.1@0.19.5",
			lock.Server.Product,
			lock.Server.Minecraft,
			lock.Server.CoreVersion,
		)
	}

	observed := workspace.Observe(root, discovery.Request{Policy: discovery.PolicyPreferModified})
	selection := observed.Runtime()
	if selection == nil {
		t.Fatalf("observation lost the declared runtime: %v", observed.SelectionError())
	}
	if got := selection.Product(); got != serverpkg.ProductFabric {
		t.Fatalf("observed product = %q, want the declared fabric runtime", got)
	}
	if selection.Installation == nil ||
		selection.Product() != serverpkg.ProductFabric ||
		selection.MinecraftVersion().String() != "1.21.1" {
		t.Fatalf(
			"observed installation = %+v, want the declared fabric runtime",
			selection.Installation,
		)
	}
}

// A declaration whose installation is gone refuses replacement, keeps the
// previous documents, and leaves live observation reporting the loss.
func TestInitializeRefusesMissingDeclaredInstallation(t *testing.T) {
	root := t.TempDir()
	launcherPath := writeFabricTestLauncher(t, root, "0.19.5", "1.21.1")
	service := newInitializeTestService(t)
	if _, err := service.Initialize(t.Context(), root, InitOptions{}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	manifestPath := filepath.Join(root, documentpkg.ManifestFilename)
	lockPath := filepath.Join(root, documentpkg.LockFilename)
	manifestBytes := readFileBytes(t, manifestPath)
	lockBytes := readFileBytes(t, lockPath)
	receiptPath := filepath.Join(root, ".lucy", "install.yaml")
	receipt := []byte("version: 1\nentries: []\n")
	if err := os.WriteFile(receiptPath, receipt, 0o644); err != nil {
		t.Fatalf("write receipt: %v", err)
	}

	if err := os.Remove(launcherPath); err != nil {
		t.Fatalf("remove declared launcher: %v", err)
	}
	writeVanillaTestJar(t, root, "server.jar", "1.21.1")

	_, err := service.Initialize(t.Context(), root, InitOptions{Force: true})
	var selectionErr *discovery.SelectionError
	if !errors.As(err, &selectionErr) {
		t.Fatalf("Initialize error = %v, want *discovery.SelectionError", err)
	}
	if selectionErr.State != discovery.SelectionMissingDeclared {
		t.Fatalf(
			"selection state = %q, want missing_declared",
			selectionErr.State,
		)
	}
	if got := readFileBytes(t, manifestPath); string(got) != string(manifestBytes) {
		t.Fatalf("refused initialization rewrote the manifest:\n%s", got)
	}
	if got := readFileBytes(t, lockPath); string(got) != string(lockBytes) {
		t.Fatalf("refused initialization rewrote the lock:\n%s", got)
	}
	if got := readFileBytes(t, receiptPath); string(got) != string(receipt) {
		t.Fatalf("refused initialization changed the receipt:\n%s", got)
	}

	observed := workspace.Observe(root, discovery.Request{Policy: discovery.PolicyPreferModified})
	if observed.Runtime() != nil {
		t.Fatalf("observation adopted a runtime for a missing declaration")
	}
	var observedErr *discovery.SelectionError
	if err := observed.SelectionError(); !errors.As(err, &observedErr) {
		t.Fatalf("observation error = %v, want *discovery.SelectionError", err)
	}
}

// An unmanaged copy of the declared runtime keeps the declaration valid even
// when the originally selected file is gone.
func TestInitializeKeepsDeclarationForUnmanagedSurvivor(t *testing.T) {
	root := t.TempDir()
	launcherPath := writeFabricTestLauncher(t, root, "0.19.5", "1.21.1")
	service := newInitializeTestService(t)
	if _, err := service.Initialize(t.Context(), root, InitOptions{}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	survivorPath := filepath.Join(root, "fabric-backup.jar")
	data := readFileBytes(t, launcherPath)
	if err := os.WriteFile(survivorPath, data, 0o644); err != nil {
		t.Fatalf("write survivor copy: %v", err)
	}
	if err := os.Remove(launcherPath); err != nil {
		t.Fatalf("remove selected launcher: %v", err)
	}

	if _, err := service.Initialize(
		t.Context(),
		root,
		InitOptions{Force: true},
	); err != nil {
		t.Fatalf("Initialize with surviving identity: %v", err)
	}
	manifest := readManifest(t, root)
	if manifest.Server.Core.Product != serverpkg.ProductFabric ||
		manifest.Server.Core.ProductVersion.String() != "0.19.5" {
		t.Fatalf(
			"manifest identity = %s@%s, want fabric@0.19.5",
			manifest.Server.Core.Product,
			manifest.Server.Core.ProductVersion,
		)
	}
	observed := workspace.Observe(root, discovery.Request{Policy: discovery.PolicyPreferModified})
	selection := observed.Runtime()
	if selection == nil {
		t.Fatalf("observation lost the surviving runtime: %v", observed.SelectionError())
	}
	if selection.Installation == nil ||
		!slices.ContainsFunc(selection.Installation.Files, func(file discovery.File) bool {
			return file.Path == survivorPath
		}) {
		t.Fatalf(
			"observed installation = %+v, want the surviving copy %s",
			selection.Installation,
			survivorPath,
		)
	}
}

// An expectation that matches no observed installation fails closed before
// any document is written.
func TestInitializeRejectsUnmatchedExpectation(t *testing.T) {
	root := t.TempDir()
	writeVanillaTestJar(t, root, "server.jar", "1.20.1")

	_, err := newInitializeTestService(t).Initialize(
		t.Context(),
		root,
		InitOptions{Expectation: &discovery.Expectation{
			Product: serverpkg.ProductPaper,
		}},
	)
	var selectionErr *discovery.SelectionError
	if !errors.As(err, &selectionErr) {
		t.Fatalf("Initialize error = %v, want *discovery.SelectionError", err)
	}
	if selectionErr.State != discovery.SelectionUnmatched {
		t.Fatalf("selection state = %q, want unmatched", selectionErr.State)
	}
	if _, err := os.Stat(filepath.Join(root, documentpkg.ManifestFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused initialization wrote a manifest: %v", err)
	}
}
