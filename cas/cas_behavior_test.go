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
