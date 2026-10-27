//go:build integration

package cas

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func casDigestOf(content []byte) Digest {
	return Digest(sha256.Sum256(content))
}

// casPhysicalBlobs reads the object namespace straight from disk and rejects
// any stored name that does not address its own bytes.
func casPhysicalBlobs(t *testing.T, root string) map[Digest][]byte {
	t.Helper()
	blobs := map[Digest][]byte{}
	err := filepath.WalkDir(filepath.Join(root, "objects"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			t.Errorf("cas object path %s is not a regular file", path)
			return nil
		}
		digest, err := ParseDigest("sha256:" + filepath.Base(path))
		if err != nil {
			t.Errorf("cas object name %s is not a digest", path)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if casDigestOf(data) != digest {
			t.Errorf("cas object %s bytes do not hash to their address", path)
		}
		if _, duplicate := blobs[digest]; duplicate {
			return fmt.Errorf("duplicate physical blob for %s", digest)
		}
		blobs[digest] = data
		return nil
	})
	if err != nil {
		t.Fatalf("walk cas objects under %s: %v", root, err)
	}
	return blobs
}

func casStagingNames(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "tmp"))
	if err != nil {
		t.Fatalf("read cas staging directory: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			t.Errorf("cas staging entry %s is not a regular file", entry.Name())
			continue
		}
		names = append(names, entry.Name())
	}
	return names
}

func casNewStore(t *testing.T, root string) *Store {
	t.Helper()
	store, err := New(root)
	if err != nil {
		t.Fatalf("open cas at %s: %v", root, err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close cas at %s: %v", root, err)
		}
	})
	return store
}

func casReadObject(t *testing.T, store *Store, digest Digest, want []byte) {
	t.Helper()
	reader, err := store.Open(digest)
	if err != nil {
		t.Fatalf("open cas object %s: %v", digest, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read cas object %s: %v", digest, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close cas object %s: %v", digest, err)
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("cas object %s read %d bytes, want declared content", digest, len(data))
	}
}

func TestCASConcurrentProcessPutsDeduplicatePhysically(t *testing.T) {
	const children = 6
	root := t.TempDir()
	store := casNewStore(t, root)

	sharedAlpha := []byte("content every child ingests\n")
	sharedBeta := bytes.Repeat([]byte("shared payload beta"), 512)

	type childOutcome struct {
		objectByName map[string]casHelperObject
		contentById  map[Digest][]byte
	}
	outcomes := make([]childOutcome, children)
	var wait sync.WaitGroup
	for index := range outcomes {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			unique := []byte(fmt.Sprintf("content only child %d ingests", index))
			response, err := runCASHelperCommand(t.Context(), casHelperRequest{
				Op:   "put",
				Root: root,
				Items: []casHelperItem{
					{Name: "shared-alpha", Content: sharedAlpha},
					{Name: "shared-beta", Content: sharedBeta},
					{Name: fmt.Sprintf("unique-%d", index), Content: unique},
				},
			})
			if err != nil {
				t.Errorf("child %d: %v", index, err)
				return
			}
			contentByName := map[string][]byte{
				"shared-alpha":                  sharedAlpha,
				"shared-beta":                   sharedBeta,
				fmt.Sprintf("unique-%d", index): unique,
			}
			outcome := childOutcome{
				objectByName: map[string]casHelperObject{},
				contentById:  map[Digest][]byte{},
			}
			for _, object := range response.Objects {
				digest, err := ParseDigest(object.Digest)
				if err != nil {
					t.Errorf("child %d reported invalid digest %q: %v", index, object.Digest, err)
					continue
				}
				if want := casDigestOf(contentByName[object.Name]); digest != want {
					t.Errorf("child %d published %s as %s", index, object.Name, digest)
				}
				if want := int64(len(contentByName[object.Name])); object.Size != want {
					t.Errorf("child %d published %s with size %d, want %d", index, object.Name, object.Size, want)
				}
				outcome.objectByName[object.Name] = object
				outcome.contentById[digest] = contentByName[object.Name]
			}
			outcomes[index] = outcome
		}(index)
	}
	wait.Wait()
	if t.Failed() {
		return
	}

	want := map[Digest][]byte{}
	for index, outcome := range outcomes {
		if len(outcome.objectByName) != 3 {
			t.Fatalf("child %d published %d objects, want 3", index, len(outcome.objectByName))
		}
		for digest, content := range outcome.contentById {
			if previous, ok := want[digest]; ok && !bytes.Equal(previous, content) {
				t.Fatalf("digest %s covers two different payloads", digest)
			}
			want[digest] = content
		}
	}
	if len(want) != children+2 {
		t.Fatalf("distinct published digests %d, want %d", len(want), children+2)
	}

	blobs := casPhysicalBlobs(t, root)
	if len(blobs) != len(want) {
		t.Fatalf("physical cas objects %d, want %d", len(blobs), len(want))
	}
	for digest, content := range want {
		data, ok := blobs[digest]
		if !ok {
			t.Fatalf("digest %s has no physical object", digest)
		}
		if !bytes.Equal(data, content) {
			t.Fatalf("physical object %s does not hold its ingested bytes", digest)
		}
		casReadObject(t, store, digest, content)
	}

	visited := map[Digest]int{}
	if err := store.Walk(func(object Object) error {
		visited[object.Digest]++
		return nil
	}); err != nil {
		t.Fatalf("walk published cas objects: %v", err)
	}
	if len(visited) != len(want) {
		t.Fatalf("cas walk visited %d objects, want %d", len(visited), len(want))
	}
	for digest, count := range visited {
		if count != 1 {
			t.Fatalf("cas walk visited %s %d times", digest, count)
		}
		if _, ok := want[digest]; !ok {
			t.Fatalf("cas walk visited unpublished object %s", digest)
		}
	}

	absent := casDigestOf([]byte("no child ever ingests this content"))
	if _, err := store.Open(absent); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("open absent cas object: %v", err)
	}
}

func TestCASCleanupSparesActiveWriters(t *testing.T) {
	root := t.TempDir()
	store := casNewStore(t, root)
	payload := []byte("bytes of a stage a live process still owns\n")

	response, held := holdCASHelperStage(t, casHelperRequest{
		Op:      "hold",
		Root:    root,
		Content: payload,
	})
	stageName := filepath.Base(response.Stage)
	leaseName := filepath.Base(response.Lease)

	if names := casStagingNames(t, root); len(names) != 2 || names[0] != stageName || names[1] != leaseName {
		t.Fatalf("live writer staging %v, want [%s %s]", names, stageName, leaseName)
	}
	staged, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(response.Stage)))
	if err != nil {
		t.Fatalf("read live writer stage: %v", err)
	}
	if !bytes.Equal(staged, payload) {
		t.Fatalf("live writer stage holds %q, want %q", staged, payload)
	}

	if err := store.Cleanup(); err != nil {
		t.Fatalf("cleanup with a leased stage: %v", err)
	}
	if names := casStagingNames(t, root); len(names) != 2 {
		t.Fatalf("cleanup reclaimed an active writer stage, staging %v", names)
	}
	staged, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(response.Stage)))
	if err != nil {
		t.Fatalf("read active writer stage after cleanup: %v", err)
	}
	if !bytes.Equal(staged, payload) {
		t.Fatalf("active writer stage changed after cleanup: %q", staged)
	}

	held.release(t, "crash", casHelperCrashCode)

	if names := casStagingNames(t, root); len(names) != 2 {
		t.Fatalf("crashed writer staging %v, want an abandoned stage and lease", names)
	}
	if blobs := casPhysicalBlobs(t, root); len(blobs) != 0 {
		t.Fatalf("crashed writer published %d objects before committing", len(blobs))
	}

	if err := store.Cleanup(); err != nil {
		t.Fatalf("cleanup after a crashed writer: %v", err)
	}
	if names := casStagingNames(t, root); len(names) != 0 {
		t.Fatalf("crashed writer stage survived cleanup, staging %v", names)
	}
	if blobs := casPhysicalBlobs(t, root); len(blobs) != 0 {
		t.Fatalf("cleanup after a crashed writer left %d objects", len(blobs))
	}
}

func TestCASCleanupRemovesDataOnlyOrphanStage(t *testing.T) {
	root := t.TempDir()
	store := casNewStore(t, root)
	payload := []byte("bytes of a stage whose lease disappeared\n")

	response := runCASHelper(t, casHelperRequest{
		Op:      "orphan",
		Root:    root,
		Content: payload,
	})
	stagePath := filepath.Join(root, filepath.FromSlash(response.Stage))
	leasePath := filepath.Join(root, filepath.FromSlash(response.Lease))
	if _, err := os.Stat(stagePath); err != nil {
		t.Fatalf("orphan stage data absent: %v", err)
	}
	if _, err := os.Stat(leasePath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("orphan stage lease stat: %v", err)
	}

	if err := store.Cleanup(); err != nil {
		t.Fatalf("cleanup with a data-only stage: %v", err)
	}
	if _, err := os.Stat(stagePath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("cleanup kept the data-only stage: %v", err)
	}
	if names := casStagingNames(t, root); len(names) != 0 {
		t.Fatalf("data-only stage cleanup left staging %v", names)
	}
}

func TestCASMaintenanceSkipsUnrelatedEntries(t *testing.T) {
	root := t.TempDir()
	store := casNewStore(t, root)
	content := []byte("retained object\n")
	object, err := store.Put(bytes.NewReader(content), nil)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(root, "objects/sha256/.DS_Store")
	if err := os.WriteFile(unrelated, []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	var visited []Object
	if err := store.Walk(func(value Object) error {
		visited = append(visited, value)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(visited) != 1 || visited[0] != object {
		t.Fatalf("walk visited %+v, want only %+v", visited, object)
	}
	if err := store.Cleanup(); err != nil {
		t.Fatal(err)
	}
	casReadObject(t, store, object.Digest, content)
}

func TestCASCleanupRemovesInterruptedReclaimGarbage(t *testing.T) {
	root := t.TempDir()
	store := casNewStore(t, root)
	content := []byte("object that outlives a failed reclamation\n")
	object, err := store.Put(bytes.NewReader(content), nil)
	if err != nil {
		t.Fatalf("put cas object: %v", err)
	}
	garbage := filepath.Join(root, "tmp", "gc-interrupted")
	if err := os.WriteFile(garbage, content, 0o600); err != nil {
		t.Fatalf("stage interrupted reclamation garbage: %v", err)
	}

	if err := store.Cleanup(); err != nil {
		t.Fatalf("cleanup with reclamation garbage: %v", err)
	}
	if _, err := os.Stat(garbage); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("cleanup kept reclamation garbage: %v", err)
	}
	casReadObject(t, store, object.Digest, content)
}

func TestCASExpectedMismatchNeverPublishes(t *testing.T) {
	root := t.TempDir()
	store := casNewStore(t, root)
	content := []byte("bytes that a caller mistrusts\n")
	wrong := casDigestOf([]byte("the bytes the caller expected instead\n"))

	writer, err := store.NewWriter()
	if err != nil {
		t.Fatalf("lease writer: %v", err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatalf("write stage: %v", err)
	}
	if _, err := writer.Commit(&wrong); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("commit with a mismatched digest: %v", err)
	}

	if blobs := casPhysicalBlobs(t, root); len(blobs) != 0 {
		t.Fatalf("mismatched commit published %d objects", len(blobs))
	}
	// A rejected seal keeps the caller's stage readable for inspection or retry.
	if err := writer.Rewind(); err != nil {
		t.Fatalf("rewind preserved stage: %v", err)
	}
	staged, err := io.ReadAll(writer)
	if err != nil {
		t.Fatalf("read preserved stage: %v", err)
	}
	if !bytes.Equal(staged, content) {
		t.Fatalf("mismatched commit lost the staged bytes: %q", staged)
	}
	if err := writer.Abort(); err != nil {
		t.Fatalf("abort writer after a mismatched commit: %v", err)
	}
	if names := casStagingNames(t, root); len(names) != 0 {
		t.Fatalf("abort after a mismatched commit left staging %v", names)
	}

	if _, err := store.Put(bytes.NewReader(content), &wrong); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("put with a mismatched digest: %v", err)
	}
	if blobs := casPhysicalBlobs(t, root); len(blobs) != 0 {
		t.Fatalf("mismatched put published %d objects", len(blobs))
	}
	if names := casStagingNames(t, root); len(names) != 0 {
		t.Fatalf("mismatched put left staging %v", names)
	}

	object, err := store.Put(bytes.NewReader(content), nil)
	if err != nil {
		t.Fatalf("put accepted bytes: %v", err)
	}
	if want := casDigestOf(content); object.Digest != want {
		t.Fatalf("accepted put published %s, want %s", object.Digest, want)
	}
	casReadObject(t, store, object.Digest, content)
}
