package filetrack

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/cas"
	documentpkg "github.com/mclucy/lucy/document"
)

func writePendingTransaction(t *testing.T, root string, txn transaction) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(transactionName)), 0o700); err != nil {
		t.Fatalf("create workspace state: %v", err)
	}
	data, err := json.Marshal(txn)
	if err != nil {
		t.Fatalf("marshal pending transaction: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, transactionName), data, 0o600); err != nil {
		t.Fatalf("write pending transaction: %v", err)
	}
}

func TestReferencesIncludesPendingTransaction(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	before := behaviorDigest([]byte("before"))
	after := behaviorDigest([]byte("after"))
	backup := behaviorDigest([]byte("backup"))
	writePendingTransaction(t, root, transaction{
		Event: Event{
			Changes: []Change{
				{
					Path:       "a.txt",
					Before:     &documentpkg.TrackedFile{Path: "a.txt", Hash: before},
					After:      &documentpkg.TrackedFile{Path: "a.txt", Hash: after},
					BackupHash: backup,
				},
			},
		},
		Mutations: []mutation{
			{
				Path:   "a.txt",
				Before: image{Exists: true, Hash: before},
				After:  image{Exists: true, Hash: after},
			},
		},
	})
	files, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	defer files.Close()
	referenced, err := references(root, files)
	if err != nil {
		t.Fatalf("references: %v", err)
	}
	for _, hash := range []string{before, after, backup} {
		digest, err := cas.ParseDigest(hash)
		if err != nil {
			t.Fatalf("parse digest %s: %v", hash, err)
		}
		if _, ok := referenced[digest]; !ok {
			t.Fatalf("references lost %s", hash)
		}
	}
}

func TestReferencesFailsClosedOnUnreadableState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{
			name: "malformed transaction",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, ".lucy"), 0o700); err != nil {
					t.Fatalf("create workspace state: %v", err)
				}
				if err := os.WriteFile(filepath.Join(root, transactionName), []byte("{"), 0o600); err != nil {
					t.Fatalf("write malformed transaction: %v", err)
				}
			},
		},
		{
			name: "malformed pending hash",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writePendingTransaction(t, root, transaction{
					Event: Event{
						Changes: []Change{
							{Path: "a.txt", BackupHash: "sha256:not-a-digest"},
						},
					},
				})
			},
		},
		{
			name: "malformed history",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, ".lucy", "history"), 0o700); err != nil {
					t.Fatalf("create history directory: %v", err)
				}
				if err := os.WriteFile(filepath.Join(root, historyName), []byte("{bad}\n"), 0o600); err != nil {
					t.Fatalf("write malformed history: %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			test.setup(t, root)
			files, err := os.OpenRoot(root)
			if err != nil {
				t.Fatalf("open workspace: %v", err)
			}
			defer files.Close()
			if _, err := references(root, files); err == nil {
				t.Fatal("references accepted an unknowable reference set")
			}
		})
	}
}

func TestReclaimObjectsKeepsReferencesAndExcludesOwnState(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := cas.New(filepath.Join(root, ".lucy"))
	if err != nil {
		t.Fatalf("open workspace store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	keep := []byte("referenced object")
	discard := []byte("unreferenced object")
	own := []byte("own working object")
	keepObject, err := store.Put(bytes.NewReader(keep), nil)
	if err != nil {
		t.Fatalf("store retained object: %v", err)
	}
	discardObject, err := store.Put(bytes.NewReader(discard), nil)
	if err != nil {
		t.Fatalf("store discarded object: %v", err)
	}
	ownObject, err := store.Put(bytes.NewReader(own), nil)
	if err != nil {
		t.Fatalf("store own object: %v", err)
	}
	writePendingTransaction(t, root, transaction{
		Event: Event{
			Changes: []Change{
				{Path: "a.txt", After: &documentpkg.TrackedFile{Path: "a.txt", Hash: keepObject.Digest.String()}},
			},
		},
	})
	files, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	defer files.Close()

	reclaimed, err := reclaimObjects(root, files, store, map[string]struct{}{
		ownObject.Digest.String(): {},
	})
	if err != nil {
		t.Fatalf("reclaimObjects: %v", err)
	}
	if reclaimed.Objects != 1 || reclaimed.Bytes != int64(len(discard)) {
		t.Fatalf("reclaimed = %+v, want only the unreferenced object", reclaimed)
	}
	if _, err := store.Open(keepObject.Digest); err != nil {
		t.Fatalf("reclaimed a referenced object: %v", err)
	}
	if _, err := store.Open(discardObject.Digest); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("kept an unreferenced object: %v", err)
	}
	if _, err := store.Open(ownObject.Digest); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("kept an unreferenced own object: %v", err)
	}
}
