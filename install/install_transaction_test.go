package install

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
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
