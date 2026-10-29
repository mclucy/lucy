package cas_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/cas"
)

func newStore(t *testing.T) *cas.Store {
	t.Helper()
	store, err := cas.New(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

func readObject(t *testing.T, store *cas.Store, digest cas.Digest) []byte {
	t.Helper()
	reader, err := store.Open(digest)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(reader)
	if err := errors.Join(readErr, reader.Close()); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStoreReusesAndPersistsContent(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "store")
	store, err := cas.New(root)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("shared artifact contents")
	first, err := store.Put(bytes.NewReader(content), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put(bytes.NewReader(content), nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatal("identical contents received different addresses")
	}
	objects := make(map[cas.Digest]bool)
	if err := store.Walk(func(object cas.Object) error {
		objects[object.Digest] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 || !objects[first.Digest] {
		t.Fatalf("unexpected stored objects: %v", objects)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = cas.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if got := readObject(t, store, first.Digest); !bytes.Equal(got, content) {
		t.Fatalf("reopened object = %q, want %q", got, content)
	}
	if _, err := store.Remove(first.Digest); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(first.Digest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed object remains accessible: %v", err)
	}
}

func TestPutRejectsUnexpectedContent(t *testing.T) {
	t.Parallel()
	store := newStore(t)
	original := []byte("expected contents")
	object, err := store.Put(bytes.NewReader(original), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(bytes.NewReader([]byte("different contents")), &object.Digest); err == nil {
		t.Fatal("put accepted content with the wrong digest")
	}
	if got := readObject(t, store, object.Digest); !bytes.Equal(got, original) {
		t.Fatalf("failed put changed existing content: %q", got)
	}
	count := 0
	if err := store.Walk(func(cas.Object) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("failed put published unexpected objects: %d", count)
	}
}

func TestSweepRetainsReferencesAndRejectsUnknownPlan(t *testing.T) {
	t.Parallel()
	store := newStore(t)
	keep, err := store.Put(bytes.NewBufferString("keep"), nil)
	if err != nil {
		t.Fatal(err)
	}
	discard, err := store.Put(bytes.NewBufferString("discard"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Sweep(func() (map[cas.Digest]struct{}, func() error, error) {
		return nil, nil, errors.New("references unavailable")
	}); err == nil {
		t.Fatal("sweep accepted an unavailable reference set")
	}
	if got := readObject(t, store, discard.Digest); string(got) != "discard" {
		t.Fatalf("failed sweep changed object: %q", got)
	}
	if _, _, err := store.Sweep(func() (map[cas.Digest]struct{}, func() error, error) {
		return map[cas.Digest]struct{}{keep.Digest: {}}, nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := readObject(t, store, keep.Digest); string(got) != "keep" {
		t.Fatalf("sweep changed retained object: %q", got)
	}
	if _, err := store.Open(discard.Digest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreferenced object remains accessible: %v", err)
	}
}
