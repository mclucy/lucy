package workspacewrite

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// A server directory named like the displacement index must not block the
// cleanup that runs after the commit point.
func TestCleanupCommittedInstallKeepsPayloadBesideIndexName(t *testing.T) {
	root := t.TempDir()
	files, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })

	content := []byte("original")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	backup := ".lucy/rollback-1"
	if err := os.MkdirAll(filepath.Join(root, backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, backup, digest), content, 0o600); err != nil {
		t.Fatal(err)
	}
	before := &InstallImage{SHA256: digest, Mode: 0o644}
	journal := &InstallJournal{
		Backup:    backup,
		Committed: true,
		Entries: []InstallJournalEntry{{
			Path:     "index.yaml/server.jar",
			Before:   before,
			After:    &InstallImage{SHA256: hex.EncodeToString(make([]byte, sha256.Size)), Mode: 0o644},
			Displace: true,
		}},
	}

	if err := CleanupCommittedInstall(files, journal); err != nil {
		t.Fatalf("CleanupCommittedInstall() error = %v", err)
	}

	directory := filepath.Join(root, DisplacedRoot, "1")
	got, err := os.ReadFile(filepath.Join(directory, DisplacedFilesName, "index.yaml", "server.jar"))
	if err != nil {
		t.Fatalf("displaced file missing: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("displaced content = %q, want %q", got, content)
	}
	if _, err := os.Stat(filepath.Join(directory, DisplacedIndexName)); err != nil {
		t.Fatalf("displacement index missing: %v", err)
	}
}
