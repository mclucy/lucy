package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
	serverpkg "github.com/mclucy/lucy/server"
)

func transactionTestDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func transactionTestWrite(t *testing.T, root, relative string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", relative, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

// An accepted entry whose file is already absent must stay in the prune plan
// as a nil precondition. Dropping it from the plan would leave the journal and
// the pre-commit check blind to the path, so a file that reappeared would no
// longer fail the install.
func TestPruneTargetsKeepsAbsentEntryAsPrecondition(t *testing.T) {
	root := t.TempDir()
	ownedBytes := []byte("owned output\n")
	transactionTestWrite(t, root, "mods/present.jar", ownedBytes)
	previous := []receipt.Entry{
		{
			Path:   "mods/absent.jar",
			SHA256: transactionTestDigest([]byte("removed output\n")),
			Origin: receipt.OriginPackage,
		},
		{
			Path:   "mods/present.jar",
			SHA256: transactionTestDigest(ownedBytes),
			Origin: receipt.OriginPackage,
		},
	}

	targets, err := pruneTargets(
		root, previous, map[string]bool{}, map[string]string{}, nil, false,
	)
	if err != nil {
		t.Fatalf("plan prunes: %v", err)
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	for _, path := range []string{"mods/absent.jar", "mods/present.jar"} {
		expected, planned := targets[path]
		if !planned {
			t.Fatalf("obsolete output %s has no guarded prune plan", path)
		}
		check := installBeforeCheck(files, workspacewrite.InstallJournalEntry{
			Path: path, Before: expected,
		})
		if err := check(); err != nil {
			t.Fatalf("unchanged prune target %s: %v", path, err)
		}
		transactionTestWrite(t, root, path, []byte("intervening user data"))
		if err := check(); err == nil {
			t.Fatalf("changed prune target %s was accepted", path)
		}
	}
}

// A commit that retains the previous bootstrap outputs because staging was
// skipped must revalidate them at the commit boundary. The currency check
// predates package staging and downloads, so a managed server output that
// drifts in that window must abort the commit before anything is written,
// instead of publishing a receipt that still reports bootstrap current.
func TestApplyCommitRetainedBootstrapDriftAborts(t *testing.T) {
	root := t.TempDir()
	core := []byte("plain server core\n")
	transactionTestWrite(t, root, "server/server.jar", core)
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: transactionTestDigest([]byte("manifest")),
		Server: document.LockedServer{
			Directory:   "server",
			Minecraft:   "1.21.4",
			Product:     serverpkg.ProductMinecraft,
			CoreVersion: "1.21.4",
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderMojang,
				ProjectID: "server",
				Version:   "1.21.4",
				Filename:  "server.jar",
				Hashes:    map[string]string{"sha256": transactionTestDigest(core)},
			},
			Java: document.LockedJava{Advisory: 21},
		},
		Packages: []document.Package{},
	}
	fingerprint, err := receipt.BootstrapFingerprint(lock)
	if err != nil {
		t.Fatalf("bootstrap fingerprint: %v", err)
	}
	owned := receipt.New()
	owned.BootstrapFingerprint = fingerprint
	owned.Entries = []receipt.Entry{{
		Path:   "server/server.jar",
		SHA256: transactionTestDigest(core),
		Mode:   0o644,
		Origin: receipt.OriginBootstrap,
	}}
	ownedBytes, err := owned.Snapshot()
	if err != nil {
		t.Fatalf("receipt snapshot: %v", err)
	}
	receiptPath := filepath.Join(root, receipt.Filename)
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o755); err != nil {
		t.Fatalf("create receipt directory: %v", err)
	}
	if err := os.WriteFile(receiptPath, ownedBytes, 0o600); err != nil {
		t.Fatalf("write receipt: %v", err)
	}

	// Nothing was restaged, so the commit retains the bootstrap outputs; an
	// undrifted tree must still commit through the retained path.
	stage := t.TempDir()
	if _, err := applyCommit(
		root, stage, nil, lock, false, nil, nil, nil, false, installCommitOverlay, nil,
	); err != nil {
		t.Fatalf("overlay commit over current bootstrap: %v", err)
	}
	committed, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("read committed receipt: %v", err)
	}

	// Drift after the currency check: the retained managed output disappears
	// while packages would be staging.
	if err := os.Remove(filepath.Join(root, "server", "server.jar")); err != nil {
		t.Fatalf("remove retained output: %v", err)
	}
	_, err = applyCommit(
		root, stage, nil, lock, false, nil, nil, nil, false, installCommitOverlay, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("commit over drifted retained bootstrap outputs: %v", err)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("reread receipt: %v", err)
	}
	if !bytes.Equal(committed, after) {
		t.Fatal("aborted commit still rewrote the receipt")
	}
}
