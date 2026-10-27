package operation

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	serverpkg "github.com/mclucy/lucy/server"
)

// renameWorkspace persists a workspace whose manifest carries every optional
// declaration a rename must preserve, then reads it back normalized so
// comparisons describe what a consumer sees.
func renameWorkspace(t *testing.T) (string, *document.Manifest) {
	t.Helper()
	root := t.TempDir()
	declared := &document.Manifest{
		SchemaVersion: document.ManifestSchemaVersion,
		Name:          "survival",
		Server: document.ManifestServer{
			Minecraft: "1.21.4",
			Directory: "server",
			Core: document.ManifestCore{
				Product:        serverpkg.ProductPaper,
				ProductVersion: "1.21.4",
			},
			Java: &document.ManifestJava{Version: 21},
			Packages: map[catalog.Ecosystem]map[string]document.Requirement{
				catalog.EcosystemPaper: {
					"modrinth:essentialsx": {
						Selector:    ">=2.20.0",
						FilePattern: "EssentialsX-*.jar",
					},
				},
			},
		},
		MCDR: &document.ManifestMCDR{
			Directory: "mcdr",
			Version:   "latest",
			Packages: map[string]document.Requirement{
				"mcdr:example": {Selector: "1.0.0"},
			},
		},
		Files: []document.TrackedFile{{
			Path: "server.properties",
			Hash: "sha256:" + strings.Repeat("a", 64),
		}},
	}
	if err := document.ManifestFile(root).Write(declared); err != nil {
		t.Fatalf("manifest.Write() error = %v", err)
	}
	persisted, err := document.ManifestFile(root).Read()
	if err != nil {
		t.Fatalf("manifest.Read() error = %v", err)
	}
	return root, persisted
}

// writeReplayLock persists a lock that replays the manifest and returns its
// exact bytes, so a rename cannot silently invalidate an offline install.
func writeReplayLock(t *testing.T, root, manifestFingerprint string) []byte {
	t.Helper()
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: manifestFingerprint,
		Server: document.LockedServer{
			Directory:   "server",
			Minecraft:   "1.21.4",
			Product:     serverpkg.ProductPaper,
			CoreVersion: "1.21.4",
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderPaperMC,
				ProjectID: "paper",
				Version:   "1.21.4",
				Filename:  "paper-1.21.4.jar",
				Hashes:    map[string]string{"sha256": strings.Repeat("b", 64)},
			},
			Bootstrapper: "paperclip",
			Java:         document.LockedJava{Advisory: 21},
		},
		Packages: []document.Package{},
	}
	if err := document.LockFile(root).Write(lock); err != nil {
		t.Fatalf("lockfile.Write() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, document.LockFilename))
	if err != nil {
		t.Fatalf("read lockfile = %v", err)
	}
	return data
}

func TestRenameServerChangesOnlyManifestName(t *testing.T) {
	root, before := renameWorkspace(t)
	fingerprintBefore, err := before.Fingerprint()
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	lockBytes := writeReplayLock(t, root, fingerprintBefore)

	previous, err := RenameServer(root, "  survival-2  ")
	if err != nil {
		t.Fatalf("RenameServer() error = %v", err)
	}
	if previous != before.Name {
		t.Fatalf("RenameServer() previous = %q, want %q", previous, before.Name)
	}

	after, err := document.ManifestFile(root).Read()
	if err != nil {
		t.Fatalf("manifest.Read() error = %v", err)
	}
	if after.Name != "survival-2" {
		t.Fatalf("manifest name = %q, want %q", after.Name, "survival-2")
	}
	expected := *before
	expected.Name = after.Name
	if !reflect.DeepEqual(*after, expected) {
		t.Fatalf(
			"RenameServer() changed declarations other than the name:\n got: %#v\nwant: %#v",
			*after,
			expected,
		)
	}

	fingerprintAfter, err := after.Fingerprint()
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	if fingerprintAfter != fingerprintBefore {
		t.Fatalf(
			"manifest fingerprint changed on rename: %q -> %q",
			fingerprintBefore,
			fingerprintAfter,
		)
	}

	persistedLock, err := os.ReadFile(filepath.Join(root, document.LockFilename))
	if err != nil {
		t.Fatalf("read lockfile = %v", err)
	}
	if !bytes.Equal(persistedLock, lockBytes) {
		t.Fatal("RenameServer() rewrote the lockfile")
	}
}

func TestRenameServerRejectsInvalidNameWithoutWriting(t *testing.T) {
	tests := []struct {
		name      string
		requested string
	}{
		{name: "empty after trimming", requested: "   "},
		{name: "control character", requested: "survival\n2"},
		{name: "invalid UTF-8", requested: "survival-\xff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, _ := renameWorkspace(t)
			manifestPath := filepath.Join(root, document.ManifestFilename)
			before, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("read manifest = %v", err)
			}

			if _, err := RenameServer(root, tt.requested); err == nil {
				t.Fatal("RenameServer() error = nil, want a name validation error")
			}

			after, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("read manifest = %v", err)
			}
			if !bytes.Equal(after, before) {
				t.Fatal("RenameServer() wrote the manifest for an invalid name")
			}
		})
	}
}

func TestRenameServerRejectsBusyWorkspaceWithoutWriting(t *testing.T) {
	root, _ := renameWorkspace(t)
	manifestPath := filepath.Join(root, document.ManifestFilename)
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest = %v", err)
	}

	writer, err := workspacewrite.Lock(root)
	if err != nil {
		t.Fatalf("workspacewrite.Lock() error = %v", err)
	}
	defer func() {
		if err := writer.Close(); err != nil {
			t.Errorf("writer.Close() error = %v", err)
		}
	}()

	if _, err := RenameServer(root, "survival-2"); err == nil {
		t.Fatal("RenameServer() error = nil, want a busy-workspace error")
	}

	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest = %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("RenameServer() wrote the manifest while another writer held the lock")
	}
}
