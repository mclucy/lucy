package filetrack

import (
	"bytes"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
)

type image struct {
	Exists bool   `json:"exists"`
	Hash   string `json:"hash,omitempty"`
	Mode   uint32 `json:"mode,omitzero"`
}

type mutation struct {
	Path   string `json:"path"`
	Before image  `json:"before"`
	After  image  `json:"after"`
}

type transaction struct {
	Event       Event      `json:"event"`
	HistorySize int64      `json:"history_size"`
	Mutations   []mutation `json:"mutations"`
}

func capture(root string, files *os.Root, store *cas.Store, name string) (result image, resultErr error) {
	if err := workspacewrite.SafePath(root, name); err != nil {
		return result, err
	}
	info, err := files.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, fmt.Errorf("file %s is not a regular file", name)
	}
	file, err := files.Open(name)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	before, err := file.Stat()
	if err != nil {
		return result, err
	}
	object, err := store.Put(file, nil)
	if err != nil {
		return result, err
	}
	after, err := file.Stat()
	if err != nil {
		return result, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return result, fmt.Errorf("file %s changed while taking its snapshot", name)
	}
	return image{Exists: true, Hash: object.Digest.String(), Mode: uint32(info.Mode().Perm())}, nil
}

func observe(files *os.Root, name string) (result image, resultErr error) {
	info, err := files.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, fmt.Errorf("file %s is not a regular file", name)
	}
	file, err := files.Open(name)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return result, err
	}
	var digest cas.Digest
	hasher.Sum(digest[:0])
	return image{Exists: true, Hash: digest.String(), Mode: uint32(info.Mode().Perm())}, nil
}

func storeBytes(store *cas.Store, data []byte, mode os.FileMode) (image, error) {
	object, err := store.Put(bytes.NewReader(data), nil)
	return image{Exists: err == nil, Hash: object.Digest.String(), Mode: uint32(mode.Perm())}, err
}

func applyImage(files *os.Root, store *cas.Store, name string, desired, expected image) error {
	check := func() error {
		current, err := observe(files, name)
		if err != nil {
			return err
		}
		if current != expected {
			return fmt.Errorf("file %s changed before publication; keep pending transaction and resolve the edit", name)
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	if !desired.Exists {
		return workspacewrite.RemoveRoot(files, name)
	}
	reader, err := imageReader(store, desired)
	if err != nil {
		return err
	}
	copyErr := fsutil.WriteRoot(files, name, reader, fsutil.WriteOptions{
		Mode:    os.FileMode(desired.Mode),
		Replace: true,
		Check:   check,
	})
	return errors.Join(copyErr, reader.Close())
}

func imageReader(store *cas.Store, state image) (io.ReadCloser, error) {
	if !state.Exists {
		return nil, os.ErrNotExist
	}
	digest, err := cas.ParseDigest(state.Hash)
	if err != nil {
		return nil, err
	}
	return store.Open(digest)
}

func ensureHistory(root string, files *os.Root) error {
	if err := workspacewrite.SafePath(root, historyName); err != nil {
		return err
	}
	if _, err := files.Lstat(historyName); errors.Is(err, os.ErrNotExist) {
		return fsutil.AtomicWrite(filepath.Join(root, historyName), nil, 0o600)
	} else {
		return err
	}
}

func appendEvent(files *os.Root, event Event) (resultErr error) {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := files.OpenFile(historyName, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func commit(root string, files *os.Root, store *cas.Store, txn transaction) (resultErr error) {
	if err := ensureHistory(root, files); err != nil {
		return err
	}
	for _, change := range txn.Mutations {
		if err := workspacewrite.SafePath(root, change.Path); err != nil {
			return err
		}
		current, err := observe(files, change.Path)
		if err != nil {
			return err
		}
		if current != change.Before {
			return fmt.Errorf("file %s changed during operation preparation", change.Path)
		}
	}
	data, err := json.Marshal(txn)
	if err != nil {
		return err
	}
	if err := fsutil.WriteRoot(files, transactionName, bytes.NewReader(data), fsutil.WriteOptions{
		Mode:    0o600,
		Replace: true,
	}); err != nil {
		return err
	}
	failed := true
	defer func() {
		if failed {
			resultErr = errors.Join(resultErr, recoverTransaction(root, files, store))
		}
	}()
	for _, change := range txn.Mutations {
		if change.Before != change.After {
			if err := applyImage(files, store, change.Path, change.After, change.Before); err != nil {
				return err
			}
		}
	}
	for _, change := range txn.Mutations {
		current, err := observe(files, change.Path)
		if err != nil {
			return err
		}
		if current != change.After {
			return fmt.Errorf("file %s changed before history commit", change.Path)
		}
	}
	if err := appendEvent(files, txn.Event); err != nil {
		return err
	}
	failed = false
	return workspacewrite.RemoveRoot(files, transactionName)
}

// RecoverPending must run under the shared workspace writer lock.
func RecoverPending(root string) (resultErr error) {
	if err := workspacewrite.SafePath(root, transactionName); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, transactionName)); errors.Is(err, os.ErrNotExist) {
		_, _, err := readHistory(root, true)
		return err
	} else if err != nil {
		return err
	}
	if err := workspacewrite.CheckInactive(root, "recovering a pending workspace transaction"); err != nil {
		return err
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	store, err := cas.New(filepath.Join(root, ".lucy"))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
	return recoverTransaction(root, files, store)
}

func recoverTransaction(root string, files *os.Root, store *cas.Store) error {
	data, err := files.ReadFile(transactionName)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var txn transaction
	if err := json.Unmarshal(data, &txn); err != nil {
		return fmt.Errorf("decode pending file transaction: %w", err)
	}
	if txn.Event.ID == "" || txn.Event.Sequence == 0 || len(txn.Mutations) == 0 || txn.HistorySize < 0 {
		return fmt.Errorf("invalid pending file transaction")
	}
	events, size, err := readHistory(root, true)
	if err != nil {
		return err
	}
	sequence := txn.Event.Sequence
	if sequence > uint64(len(events)+1) {
		return fmt.Errorf("pending transaction has an invalid history sequence")
	}
	prior := events
	if sequence <= uint64(len(events)) {
		prior = events[:sequence-1]
	}
	if err := validateEvent(prior, txn.Event); err != nil {
		return fmt.Errorf("invalid pending transaction event: %w", err)
	}
	committed := false
	for _, event := range events {
		if event.ID == txn.Event.ID {
			expected, err := json.Marshal(txn.Event)
			if err != nil {
				return err
			}
			actual, err := json.Marshal(event)
			if err != nil || !bytes.Equal(actual, expected) {
				return fmt.Errorf("pending transaction conflicts with committed history")
			}
			committed = true
		}
	}
	if committed && sequence != uint64(len(events)) {
		return fmt.Errorf("pending transaction does not describe the latest history event")
	}
	if !committed && sequence != uint64(len(events)+1) {
		return fmt.Errorf("pending transaction conflicts with the history sequence")
	}
	if !committed && size != txn.HistorySize {
		return fmt.Errorf("file history changed after an interrupted transaction")
	}
	for _, change := range txn.Mutations {
		if change.Path != manifest.Filename && change.Path != lockfile.Filename {
			if normalized, err := input.TrackedPath(change.Path); err != nil || normalized != change.Path {
				return fmt.Errorf("invalid pending file transaction path")
			}
		}
		if err := workspacewrite.SafePath(root, change.Path); err != nil {
			return err
		}
		for _, state := range []image{change.Before, change.After} {
			if state.Exists {
				if _, err := cas.ParseDigest(state.Hash); err != nil || state.Mode&^uint32(0o777) != 0 {
					return fmt.Errorf("invalid pending file pre-image")
				}
			}
		}
		current, err := observe(files, change.Path)
		if err != nil {
			return err
		}
		if current != change.Before && current != change.After {
			return fmt.Errorf("file %s changed after interruption; keep pending transaction and resolve the edit", change.Path)
		}
	}
	for _, change := range txn.Mutations {
		desired := change.Before
		if committed {
			desired = change.After
		}
		current, err := observe(files, change.Path)
		if err != nil {
			return err
		}
		if current != change.Before && current != change.After {
			return fmt.Errorf("file %s changed during interrupted transaction recovery; keep pending transaction and resolve the edit", change.Path)
		}
		if current != desired {
			if err := applyImage(files, store, change.Path, desired, current); err != nil {
				return err
			}
		}
	}
	if committed {
		file, err := files.OpenFile(historyName, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		syncErr := file.Sync()
		if closeErr := file.Close(); syncErr != nil || closeErr != nil {
			return errors.Join(syncErr, closeErr)
		}
	}
	return workspacewrite.RemoveRoot(files, transactionName)
}
