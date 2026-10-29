package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestIndexLoadCreatesAndReusesManifest(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cache.json")
	index := newIndex(path)
	reset, err := index.load()
	if err != nil {
		t.Fatalf("load missing index: %v", err)
	}
	if !reset {
		t.Fatal("missing index was not reported as a reset")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("missing index was not created: %v", err)
	}
	reset, err = index.load()
	if err != nil {
		t.Fatalf("reload unchanged index: %v", err)
	}
	if reset {
		t.Fatal("unchanged index was reported as a reset")
	}
}

func TestIndexLoadResetsInvalidManifests(t *testing.T) {
	t.Parallel()
	validEntry := func() *Entry {
		return &Entry{
			Kind:        KindArtifact,
			Filename:    "file.jar",
			Size:        1,
			ContentHash: testDigest("entry"),
			Expiration:  time.Now().Add(time.Hour),
		}
	}
	tests := []struct {
		name     string
		manifest indexManifest
	}{
		{
			name:     "wrong version",
			manifest: indexManifest{Version: indexVersion - 1, Entries: map[key]*Entry{}},
		},
		{
			name:     "nil entries",
			manifest: indexManifest{Version: indexVersion, Entries: nil},
		},
		{
			name: "nil entry",
			manifest: indexManifest{
				Version: indexVersion,
				Entries: map[key]*Entry{"key": nil},
			},
		},
		{
			name: "negative size",
			manifest: indexManifest{
				Version: indexVersion,
				Entries: map[key]*Entry{
					"key": func() *Entry {
						entry := validEntry()
						entry.Size = -1
						entry.Key = "key"
						return entry
					}(),
				},
			},
		},
		{
			name: "unknown kind",
			manifest: indexManifest{
				Version: indexVersion,
				Entries: map[key]*Entry{
					"key": func() *Entry {
						entry := validEntry()
						entry.Kind = EntryKind(255)
						entry.Key = "key"
						return entry
					}(),
				},
			},
		},
		{
			name: "key mismatch",
			manifest: indexManifest{
				Version: indexVersion,
				Entries: map[key]*Entry{
					"key": func() *Entry {
						entry := validEntry()
						entry.Key = "other"
						return entry
					}(),
				},
			},
		},
		{
			name: "bad content hash",
			manifest: indexManifest{
				Version: indexVersion,
				Entries: map[key]*Entry{
					"key": func() *Entry {
						entry := validEntry()
						entry.ContentHash = "sha256:not-a-digest"
						entry.Key = "key"
						return entry
					}(),
				},
			},
		},
		{
			name: "empty filename",
			manifest: indexManifest{
				Version: indexVersion,
				Entries: map[key]*Entry{
					"key": func() *Entry {
						entry := validEntry()
						entry.Filename = ""
						entry.Key = "key"
						return entry
					}(),
				},
			},
		},
		{
			name: "unsafe filename",
			manifest: indexManifest{
				Version: indexVersion,
				Entries: map[key]*Entry{
					"key": func() *Entry {
						entry := validEntry()
						entry.Filename = "../file.jar"
						entry.Key = "key"
						return entry
					}(),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "cache.json")
			data, err := json.Marshal(test.manifest)
			if err != nil {
				t.Fatalf("marshal manifest: %v", err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			index := newIndex(path)
			reset, err := index.load()
			if err != nil {
				t.Fatalf("load invalid manifest: %v", err)
			}
			if !reset {
				t.Fatal("invalid manifest was not reported as a reset")
			}
			if len(index.entries) != 0 {
				t.Fatalf("reset index holds %d entries", len(index.entries))
			}
			var rewritten indexManifest
			file, err := os.Open(path)
			if err != nil {
				t.Fatalf("open rewritten manifest: %v", err)
			}
			decodeErr := json.NewDecoder(file).Decode(&rewritten)
			closeErr := file.Close()
			if err := errors.Join(decodeErr, closeErr); err != nil {
				t.Fatalf("decode rewritten manifest: %v", err)
			}
			if rewritten.Version != indexVersion || len(rewritten.Entries) != 0 {
				t.Fatalf("rewritten manifest = %+v, want version %d and no entries", rewritten, indexVersion)
			}
		})
	}
}

func TestIndexFlushRejectsUnwritablePath(t *testing.T) {
	t.Parallel()
	index := newIndex(t.TempDir())
	if err := index.flush(); err == nil {
		t.Fatal("flush accepted a directory as the index path")
	}
}

func TestIndexExistsAndAll(t *testing.T) {
	t.Parallel()
	index := newIndex(filepath.Join(t.TempDir(), "cache.json"))
	entry := &Entry{Key: "key", Filename: "file.jar"}
	index.put("key", entry)
	if !index.exists("key") {
		t.Fatal("exists missed a stored key")
	}
	if index.exists("missing") {
		t.Fatal("exists found an absent key")
	}
	all := index.all()
	if len(all) != 1 || all["key"] != entry {
		t.Fatalf("all = %+v, want the stored entry", all)
	}
	index.delete("key")
	if index.exists("key") {
		t.Fatal("delete kept the key")
	}
}
