package cas

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCASWriterSealGuardsWritesAndPublishesStage(t *testing.T) {
	root := t.TempDir()
	store := casNewStore(t, root)
	content := []byte("bytes a caller seals before publishing\n")
	want := casDigestOf(content)

	writer, err := store.NewWriter()
	if err != nil {
		t.Fatalf("lease writer: %v", err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatalf("write stage: %v", err)
	}
	object, err := writer.Seal(nil)
	if err != nil {
		t.Fatalf("seal stage: %v", err)
	}
	if object.Digest != want || object.Size != int64(len(content)) {
		t.Fatalf("sealed stage describes %s of %d bytes, want %s of %d", object.Digest, object.Size, want, len(content))
	}
	if _, err := writer.Write([]byte("late append")); err == nil {
		t.Fatalf("sealed stage accepted another write")
	}
	if _, err := writer.Seal(&want); err != nil {
		t.Fatalf("seal again with the matching digest: %v", err)
	}
	wrong := casDigestOf([]byte("other content"))
	if _, err := writer.Seal(&wrong); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("seal again with a mismatched digest: %v", err)
	}
	if err := writer.Rewind(); err != nil {
		t.Fatalf("rewind sealed stage: %v", err)
	}
	sealed, err := io.ReadAll(writer)
	if err != nil {
		t.Fatalf("read sealed stage: %v", err)
	}
	if !bytes.Equal(sealed, content) {
		t.Fatalf("sealed stage reads %q, want %q", sealed, content)
	}

	committed, err := writer.Commit(nil)
	if err != nil {
		t.Fatalf("commit sealed stage: %v", err)
	}
	if committed.Digest != want {
		t.Fatalf("commit published %s, want %s", committed.Digest, want)
	}
	blobs := casPhysicalBlobs(t, root)
	if len(blobs) != 1 || !bytes.Equal(blobs[want], content) {
		t.Fatalf("commit left physical objects %v", blobs)
	}
	casReadObject(t, store, want, content)

	if err := writer.Abort(); err != nil {
		t.Fatalf("abort after commit: %v", err)
	}
	if err := writer.Abort(); err != nil {
		t.Fatalf("abort again after commit: %v", err)
	}
	if names := casStagingNames(t, root); len(names) != 0 {
		t.Fatalf("abort after commit left staging %v", names)
	}
	if blobs := casPhysicalBlobs(t, root); len(blobs) != 1 || !bytes.Equal(blobs[want], content) {
		t.Fatalf("abort after commit disturbed published bytes: %v", blobs)
	}
}

func TestCASWriterMoveToContinuesPrefixAndHash(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "source")
	destinationRoot := filepath.Join(t.TempDir(), "destination")
	source := casNewStore(t, sourceRoot)
	destination := casNewStore(t, destinationRoot)

	prefix := []byte("prefix bytes a writer cannot keep in place\n")
	remainder := []byte("remainder bytes that continue the same stream\n")
	want := casDigestOf(append(append([]byte{}, prefix...), remainder...))

	writer, err := source.NewWriter()
	if err != nil {
		t.Fatalf("lease writer: %v", err)
	}
	if _, err := writer.Write(prefix); err != nil {
		t.Fatalf("write stage prefix: %v", err)
	}
	moved, err := writer.MoveTo(destination)
	if err != nil {
		t.Fatalf("move stage to another store: %v", err)
	}

	if _, err := writer.Write(remainder); err == nil {
		t.Fatalf("moved writer accepted another write")
	}

	if _, err := moved.Write(remainder); err != nil {
		t.Fatalf("continue the moved stage: %v", err)
	}
	object, err := moved.Commit(nil)
	if err != nil {
		t.Fatalf("commit the moved stage: %v", err)
	}
	if object.Digest != want {
		t.Fatalf("moved stage committed %s, want %s", object.Digest, want)
	}
	if object.Size != int64(len(prefix)+len(remainder)) {
		t.Fatalf("moved stage committed %d bytes, want %d", object.Size, len(prefix)+len(remainder))
	}
	casReadObject(t, destination, want, append(append([]byte{}, prefix...), remainder...))

	if _, err := source.Open(want); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source store publishes the moved object: %v", err)
	}
	if err := writer.Abort(); err != nil {
		t.Fatalf("abort the moved writer: %v", err)
	}
	if names := casStagingNames(t, sourceRoot); len(names) != 0 {
		t.Fatalf("aborted moved writer left staging %v", names)
	}
	if blobs := casPhysicalBlobs(t, sourceRoot); len(blobs) != 0 {
		t.Fatalf("source store holds %d objects after the move", len(blobs))
	}
	if names := casStagingNames(t, destinationRoot); len(names) != 0 {
		t.Fatalf("committed moved writer left staging %v", names)
	}
	if err := moved.Abort(); err != nil {
		t.Fatalf("abort the moved writer twice: %v", err)
	}
}
