package operation

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/install"
	serverpkg "github.com/mclucy/lucy/server"
	"gopkg.in/yaml.v3"
)

// compositeWorkspace seeds a current native installation whose receipt owns
// only the server core. Tracked-file changes must never alter that runtime
// ownership or the pins the lock records.
func compositeWorkspace(t *testing.T) (string, *document.Manifest, *document.Lock, []byte) {
	t.Helper()
	root := t.TempDir()
	manifest := &document.Manifest{
		SchemaVersion: document.ManifestSchemaVersion,
		Name:          "test",
		Server: document.ManifestServer{
			Minecraft: "1.21.4",
			Directory: "server",
			Core:      document.ManifestCore{Product: serverpkg.ProductMinecraft},
			Packages:  map[catalog.Ecosystem]map[string]document.Requirement{},
		},
	}
	if err := document.ManifestFile(root).Write(manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	fingerprint, err := manifest.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint manifest: %v", err)
	}
	core := []byte("composite core bytes\n")
	operationTestWrite(t, root, "server/server.jar", core, 0o644)
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
				Hashes:   map[string]string{"sha256": operationTestDigest(core)},
			},
			Java: document.LockedJava{Advisory: 21},
		},
		Packages: []document.Package{},
	}
	if err := document.LockFile(root).Write(lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	compositeWriteReceipt(t, root, lock)
	return root, manifest, lock, core
}

func compositeWriteReceipt(t *testing.T, root string, lock *document.Lock) {
	t.Helper()
	fingerprint, err := install.ResolutionFingerprint(lock)
	if err != nil {
		t.Fatalf("resolution fingerprint: %v", err)
	}
	core := operationTestRead(t, root, "server/server.jar")
	receipt := struct {
		Version         int    `yaml:"version"`
		LockFingerprint string `yaml:"lock_fingerprint"`
		Entries         []struct {
			Path   string `yaml:"path"`
			SHA256 string `yaml:"sha256"`
			Mode   uint32 `yaml:"mode"`
		} `yaml:"entries"`
	}{
		Version:         1,
		LockFingerprint: fingerprint,
	}
	receipt.Entries = append(receipt.Entries, struct {
		Path   string `yaml:"path"`
		SHA256 string `yaml:"sha256"`
		Mode   uint32 `yaml:"mode"`
	}{Path: "server/server.jar", SHA256: operationTestDigest(core), Mode: 0o644})
	data, err := yaml.Marshal(receipt)
	if err != nil {
		t.Fatalf("marshal receipt: %v", err)
	}
	operationTestWrite(t, root, ".lucy/install.yaml", data, 0o600)
}

func compositeServices(t *testing.T) (*filetrack.Service, *install.Service, *Service) {
	t.Helper()
	tracker := filetrack.NewService(&http.Client{}, nil)
	installer := install.NewService(nil, nil, tracker, nil, operationTestRegistry(t), nil, "")
	return tracker, installer, NewService(installer, nil, nil, nil)
}

func compositeManifestFingerprint(t *testing.T, root string) string {
	t.Helper()
	manifest, err := document.ManifestFile(root).Read()
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	fingerprint, err := manifest.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint manifest: %v", err)
	}
	return fingerprint
}

func TestFileCommandsPreserveRuntimePinsAndReceipt(t *testing.T) {
	isolateOperationHome(t)
	root, _, lock, core := compositeWorkspace(t)
	tracker, _, service := compositeServices(t)

	rules := []byte("server rules v1\n")
	operationTestWrite(t, root, "config/rules.txt", rules, 0o644)
	if _, err := tracker.Run(t.Context(), root, filetrack.Request{
		Operation: filetrack.Track,
		Paths:     []string{"config/rules.txt"},
	}); err != nil {
		t.Fatalf("track file: %v", err)
	}
	current, err := document.LockFile(root).Read()
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if !reflect.DeepEqual(current.Server, lock.Server) {
		t.Fatalf("a file change rewrote the runtime pins:\n%+v\n%+v", current.Server, lock.Server)
	}
	if current.ManifestFingerprint != compositeManifestFingerprint(t, root) {
		t.Fatalf("the lock is not current after the file change: %s", current.ManifestFingerprint)
	}
	if len(current.Files) != 1 || current.Files[0].Path != "config/rules.txt" ||
		current.Files[0].Immutable {
		t.Fatalf("the lock did not publish the declaration: %+v", current.Files)
	}
	manifestBytes := operationTestRead(t, root, document.ManifestFilename)
	lockBytes := operationTestRead(t, root, document.LockFilename)
	receiptBytes := operationTestRead(t, root, ".lucy/install.yaml")

	// The permissions a user chose for the documents must survive each rewrite.
	for _, name := range []string{document.ManifestFilename, document.LockFilename} {
		if err := os.Chmod(filepath.Join(root, name), 0o640); err != nil {
			t.Fatalf("chmod %s: %v", name, err)
		}
	}
	operationTestWrite(t, root, "config/extra.txt", []byte("extra note\n"), 0o644)
	if _, err := tracker.Run(t.Context(), root, filetrack.Request{
		Operation: filetrack.Track,
		Paths:     []string{"config/extra.txt"},
	}); err != nil {
		t.Fatalf("track second file: %v", err)
	}
	for _, name := range []string{document.ManifestFilename, document.LockFilename} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Fatalf("%s mode = %v, want 0640", name, info.Mode().Perm())
		}
	}
	fresh, err := document.LockFile(root).Read()
	if err != nil {
		t.Fatalf("read fresh lock: %v", err)
	}
	if len(fresh.Files) != 2 || fresh.ManifestFingerprint != compositeManifestFingerprint(t, root) {
		t.Fatalf("the manifest-only edit did not publish a current lock: %+v", fresh.Files)
	}
	if !reflect.DeepEqual(fresh.Server, lock.Server) {
		t.Fatal("a manifest-only edit rewrote the runtime pins")
	}
	manifestBytes = operationTestRead(t, root, document.ManifestFilename)
	lockBytes = operationTestRead(t, root, document.LockFilename)

	coreInfo, err := os.Stat(filepath.Join(root, "server", "server.jar"))
	if err != nil {
		t.Fatalf("stat installed core: %v", err)
	}
	result, err := service.Sync(t.Context(), root, install.Options{Locked: true, Offline: true})
	if err != nil {
		t.Fatalf("no-op install: %v", err)
	}
	if result.Files.Created != 0 || result.Files.Replaced != 0 || result.Files.Removed != 0 {
		t.Fatalf("a no-op install reported changes: %+v", result.Files)
	}
	info, err := os.Stat(filepath.Join(root, "server", "server.jar"))
	if err != nil {
		t.Fatalf("stat replayed core: %v", err)
	}
	if !info.ModTime().Equal(coreInfo.ModTime()) {
		t.Fatal("a no-op install touched the installed core")
	}
	if !operationTestBytesEqual(t, root, "server/server.jar", core) {
		t.Fatal("a no-op install rewrote the installed core")
	}
	if !operationTestBytesEqual(t, root, document.ManifestFilename, manifestBytes) ||
		!operationTestBytesEqual(t, root, document.LockFilename, lockBytes) ||
		!operationTestBytesEqual(t, root, ".lucy/install.yaml", receiptBytes) {
		t.Fatal("a no-op install rewrote a document it did not own")
	}
}

func TestInstallProvisionsMissingTrackedFileFromCAS(t *testing.T) {
	isolateOperationHome(t)
	root, _, _, core := compositeWorkspace(t)
	tracker, _, service := compositeServices(t)
	rules := []byte("server rules v2\n")
	operationTestWrite(t, root, "config/rules.txt", rules, 0o644)
	if _, err := tracker.Run(t.Context(), root, filetrack.Request{
		Operation: filetrack.Track,
		Paths:     []string{"config/rules.txt"},
	}); err != nil {
		t.Fatalf("track file: %v", err)
	}
	coreInfo, err := os.Stat(filepath.Join(root, "server", "server.jar"))
	if err != nil {
		t.Fatalf("stat installed core: %v", err)
	}

	for _, offline := range []bool{false, true} {
		t.Run(fmt.Sprintf("offline=%t", offline), func(t *testing.T) {
			if err := os.Remove(filepath.Join(root, "config/rules.txt")); err != nil {
				t.Fatalf("remove the tracked file: %v", err)
			}
			if _, err := service.Sync(t.Context(), root, install.Options{Locked: true, Offline: offline}); err != nil {
				t.Fatalf("install provisioning: %v", err)
			}
			if !operationTestBytesEqual(t, root, "config/rules.txt", rules) {
				t.Fatal("install did not provision the accepted revision")
			}
			info, err := os.Stat(filepath.Join(root, "server", "server.jar"))
			if err != nil {
				t.Fatalf("stat installed core: %v", err)
			}
			if !info.ModTime().Equal(coreInfo.ModTime()) || !operationTestBytesEqual(t, root, "server/server.jar", core) {
				t.Fatal("provisioning touched the installed core")
			}
			statuses, err := filetrack.Inspect(root, []document.TrackedFile{{
				Path: "config/rules.txt",
				Hash: "sha256:" + operationTestDigest(rules),
			}})
			if err != nil {
				t.Fatalf("inspect provisioned file: %v", err)
			}
			if len(statuses) != 1 || statuses[0].State != "matching" {
				t.Fatalf("provisioned file is not the accepted revision: %+v", statuses)
			}
		})
	}
}

func TestInstallReportsImmutableDriftWithoutRestoring(t *testing.T) {
	isolateOperationHome(t)
	root, _, _, core := compositeWorkspace(t)
	tracker, _, service := compositeServices(t)
	rules := []byte("server rules v3\n")
	operationTestWrite(t, root, "config/rules.txt", rules, 0o644)
	if _, err := tracker.Run(t.Context(), root, filetrack.Request{
		Operation: filetrack.Track,
		Paths:     []string{"config/rules.txt"},
	}); err != nil {
		t.Fatalf("track file: %v", err)
	}
	if _, err := tracker.Run(t.Context(), root, filetrack.Request{
		Operation: filetrack.Lock,
		Paths:     []string{"config/rules.txt"},
	}); err != nil {
		t.Fatalf("lock file: %v", err)
	}
	coreInfo, err := os.Stat(filepath.Join(root, "server", "server.jar"))
	if err != nil {
		t.Fatalf("stat installed core: %v", err)
	}
	drifted := []byte("drifted rules v3\n")
	operationTestWrite(t, root, "config/rules.txt", drifted, 0o644)

	result, err := service.Sync(t.Context(), root, install.Options{Locked: true, Offline: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	warned := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Path == "config/rules.txt" {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("install omitted the immutable deviation diagnostic: %+v", result.Diagnostics)
	}
	if !operationTestBytesEqual(t, root, "config/rules.txt", drifted) {
		t.Fatal("install restored a drifted immutable file")
	}
	info, err := os.Stat(filepath.Join(root, "server", "server.jar"))
	if err != nil {
		t.Fatalf("stat installed core: %v", err)
	}
	if !info.ModTime().Equal(coreInfo.ModTime()) || !operationTestBytesEqual(t, root, "server/server.jar", core) {
		t.Fatal("the warned install touched the installed core")
	}
	statuses, err := filetrack.Inspect(root, []document.TrackedFile{{
		Path:      "config/rules.txt",
		Immutable: true,
		Hash:      "sha256:" + operationTestDigest(rules),
	}})
	if err != nil {
		t.Fatalf("inspect drift: %v", err)
	}
	if len(statuses) != 1 || statuses[0].State != "modified" || !statuses[0].Immutable {
		t.Fatalf("install changed the reported drift state: %+v", statuses)
	}
}

func TestFileOnlyChangeKeepsStaleLockStale(t *testing.T) {
	isolateOperationHome(t)
	root, _, lock, core := compositeWorkspace(t)
	tracker, _, service := compositeServices(t)
	stale := *lock
	stale.ManifestFingerprint = "sha256:" + strings.Repeat("a", 64)
	if err := document.LockFile(root).Write(&stale); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}
	receiptBytes := operationTestRead(t, root, ".lucy/install.yaml")

	rules := []byte("server rules v4\n")
	operationTestWrite(t, root, "config/rules.txt", rules, 0o644)
	if _, err := tracker.Run(t.Context(), root, filetrack.Request{
		Operation: filetrack.Track,
		Paths:     []string{"config/rules.txt"},
	}); err != nil {
		t.Fatalf("track file: %v", err)
	}
	if _, err := tracker.Run(t.Context(), root, filetrack.Request{
		Operation: filetrack.Lock,
		Paths:     []string{"config/rules.txt"},
	}); err != nil {
		t.Fatalf("lock file: %v", err)
	}

	fresh, err := document.LockFile(root).Read()
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if fresh.ManifestFingerprint != stale.ManifestFingerprint {
		t.Fatalf("a file-only change stamped a fresh fingerprint onto a stale lock: %s", fresh.ManifestFingerprint)
	}
	if len(fresh.Files) != 1 || fresh.Files[0].Path != "config/rules.txt" || !fresh.Files[0].Immutable {
		t.Fatalf("the file-only change lost the declaration: %+v", fresh.Files)
	}
	if !reflect.DeepEqual(fresh.Server, lock.Server) {
		t.Fatal("the file-only change rewrote the runtime pins")
	}
	if !operationTestBytesEqual(t, root, ".lucy/install.yaml", receiptBytes) {
		t.Fatal("the file-only change rewrote the installation receipt")
	}

	for _, options := range []install.Options{{Offline: true}, {Locked: true}} {
		if _, err := service.Sync(t.Context(), root, options); err == nil {
			t.Fatalf("install with %+v accepted a stale lock", options)
		}
	}
	if !operationTestBytesEqual(t, root, ".lucy/install.yaml", receiptBytes) {
		t.Fatal("a rejected install rewrote the installation receipt")
	}
	if !operationTestBytesEqual(t, root, "server/server.jar", core) {
		t.Fatal("a rejected install rewrote the installed core")
	}
}

func operationTestBytesEqual(t *testing.T, root, relative string, want []byte) bool {
	t.Helper()
	actual, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return string(actual) == string(want)
}
