package filetrack

import (
	"encoding/json/v2"
	"errors"
	"io"
	"os"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/types"
)

// Reclaimed counts content store objects that a sweep removed because no
// accepted state names them.
type Reclaimed struct {
	Objects int   `json:"objects,omitzero"`
	Bytes   int64 `json:"bytes,omitzero"`
}

// reclaimObjects deletes objects outside the reference set of history, the
// manifest, the lockfile, and any pending transaction. History never forgets a
// revision, so a digest that any event named stays revertible.
//
// own holds the objects this run stored for its own transaction images. They
// are swept like any other leftover but not counted: they are the run's working
// state, not garbage it inherited.
//
// Callers hold the workspace writer lock: no command can be between storing an
// object and recording it.
func reclaimObjects(root string, files *os.Root, store *cas.Store, own map[string]struct{}) (Reclaimed, error) {
	var reclaimed Reclaimed
	referenced, err := references(root, files)
	if err != nil {
		return reclaimed, err
	}
	var failures []error
	walkErr := store.Walk(func(object cas.Object) error {
		if _, ok := referenced[object.Digest]; ok {
			return nil
		}
		size, err := store.Remove(object.Digest)
		if err != nil {
			failures = append(failures, err)
			return nil
		}
		if _, counted := own[object.Digest.String()]; counted {
			return nil
		}
		reclaimed.Objects++
		reclaimed.Bytes += size
		return nil
	})
	return reclaimed, errors.Join(append(failures, walkErr)...)
}

// references collects every digest that a command may open later. A malformed
// hash fails the sweep: an unknowable reference set must not authorize
// deletion.
func references(root string, files *os.Root) (map[cas.Digest]struct{}, error) {
	events, _, err := readHistory(root, false)
	if err != nil {
		return nil, err
	}
	referenced := make(map[cas.Digest]struct{})
	for _, event := range events {
		if err := addChangeReferences(referenced, event.Changes); err != nil {
			return nil, err
		}
	}
	if err := addDocumentReferences(
		files,
		manifest.Filename,
		func(reader io.Reader) ([]types.TrackedFile, error) {
			document, err := manifest.Decode(reader)
			if err != nil {
				return nil, err
			}
			return document.Files, nil
		},
		referenced,
	); err != nil {
		return nil, err
	}
	if err := addDocumentReferences(
		files,
		lockfile.Filename,
		func(reader io.Reader) ([]types.TrackedFile, error) {
			document, err := lockfile.Decode(reader)
			if err != nil {
				return nil, err
			}
			return document.Files, nil
		},
		referenced,
	); err != nil {
		return nil, err
	}
	if err := addPendingReferences(files, referenced); err != nil {
		return nil, err
	}
	return referenced, nil
}

// addPendingReferences keeps a transaction that recovery may still finish or
// roll back from losing the objects it names.
func addPendingReferences(files *os.Root, referenced map[cas.Digest]struct{}) error {
	data, err := files.ReadFile(transactionName)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var txn transaction
	if err := json.Unmarshal(data, &txn); err != nil {
		return err
	}
	for _, change := range txn.Mutations {
		for _, state := range []image{change.Before, change.After} {
			if err := addHash(referenced, state.Hash); err != nil {
				return err
			}
		}
	}
	return addChangeReferences(referenced, txn.Event.Changes)
}

func addDocumentReferences(
	files *os.Root,
	name string,
	decode func(io.Reader) ([]types.TrackedFile, error),
	referenced map[cas.Digest]struct{},
) error {
	reader, err := files.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	declarations, decodeErr := decode(reader)
	closeErr := reader.Close()
	if err := errors.Join(decodeErr, closeErr); err != nil {
		return err
	}
	for _, declaration := range declarations {
		if err := addHash(referenced, declaration.Hash); err != nil {
			return err
		}
	}
	return nil
}

func addChangeReferences(referenced map[cas.Digest]struct{}, changes []Change) error {
	for _, change := range changes {
		for _, state := range []*types.TrackedFile{change.Before, change.After} {
			if state == nil {
				continue
			}
			if err := addHash(referenced, state.Hash); err != nil {
				return err
			}
		}
		if err := addHash(referenced, change.BackupHash); err != nil {
			return err
		}
	}
	return nil
}

func addHash(referenced map[cas.Digest]struct{}, hash string) error {
	if hash == "" {
		return nil
	}
	digest, err := cas.ParseDigest(hash)
	if err != nil {
		return err
	}
	referenced[digest] = struct{}{}
	return nil
}
