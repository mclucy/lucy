package cas

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/internal/fsutil"
)

// Writer has one owner. Seal prevents further writes; Abort releases an unpublished stage.
type Writer struct {
	store     *Store
	file      *os.File
	lease     *fsutil.FileLock
	dataName  string
	leaseName string
	hash      hash.Hash
	size      int64
	sealed    bool
	done      bool
	detached  bool
	object    Object
}

// NewWriter leases a unique stage so cleanup cannot remove active writes.
func (s *Store) NewWriter() (*Writer, error) {
	return s.newWriter(sha256.New())
}

func (s *Store) newWriter(hasher hash.Hash) (writer *Writer, resultErr error) {
	lock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	name := "tmp/stage-" + rand.Text()
	leaseName := name + ".lease"
	leaseFile, err := s.root.OpenFile(leaseName, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	lease, err := fsutil.LockOpenedFile(leaseFile, false)
	if err != nil {
		return nil, errors.Join(err, s.root.Remove(leaseName))
	}
	file, err := s.root.OpenFile(name+".data", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, errors.Join(err, lease.Close(), s.root.Remove(leaseName))
	}
	return &Writer{store: s, file: file, lease: lease, dataName: name + ".data", leaseName: leaseName, hash: hasher}, nil
}

func (w *Writer) Write(data []byte) (int, error) {
	if w.done || w.sealed {
		return 0, fmt.Errorf("cas writer is closed for writes")
	}
	count, err := w.file.Write(data)
	if count > 0 {
		if _, hashErr := w.hash.Write(data[:count]); hashErr != nil {
			return count, errors.Join(err, hashErr)
		}
		w.size += int64(count)
	}
	return count, err
}

// Seal checks an optional expected digest and makes the stage readable without publishing it.
func (w *Writer) Seal(expected *Digest) (Object, error) {
	if w.done || w.detached {
		return Object{}, os.ErrClosed
	}
	if !w.sealed {
		var digest Digest
		w.hash.Sum(digest[:0])
		w.object = Object{Digest: digest, Size: w.size}
		if err := w.file.Sync(); err != nil {
			return Object{}, fmt.Errorf("sync cas stage: %w", err)
		}
		w.sealed = true
	}
	if expected != nil && w.object.Digest != *expected {
		return Object{}, fmt.Errorf("%w: expected %s, got %s", ErrCorrupt, expected.String(), w.object.Digest.String())
	}
	return w.object, nil
}

// Rewind is available only after Seal; reading does not hash or copy the stage again.
func (w *Writer) Rewind() error {
	if !w.sealed || w.done {
		return fmt.Errorf("cas stage must be sealed before reading")
	}
	_, err := w.file.Seek(0, io.SeekStart)
	return err
}

func (w *Writer) Read(data []byte) (int, error) {
	if !w.sealed || w.done {
		return 0, fmt.Errorf("cas stage must be sealed before reading")
	}
	return w.file.Read(data)
}

// Commit publishes only the sealed contents. It can repair corrupt bytes at the same address.
func (w *Writer) Commit(expected *Digest) (object Object, resultErr error) {
	object, err := w.Seal(expected)
	if err != nil {
		return Object{}, err
	}
	lock, err := w.store.lock()
	if err != nil {
		return Object{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	name := objectPath(object.Digest)
	if info, err := w.store.root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return Object{}, fmt.Errorf("invalid existing cas object")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Object{}, err
	}
	if err := w.store.root.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return Object{}, err
	}
	if err := w.file.Chmod(0o400); err != nil {
		return Object{}, err
	}
	if err := w.file.Sync(); err != nil {
		return Object{}, err
	}
	if err := w.file.Close(); err != nil {
		w.file = nil
		return Object{}, err
	}
	w.file = nil
	if err := fsutil.RenameRoot(w.store.root, w.dataName, name); err != nil {
		return Object{}, fmt.Errorf("publish cas object: %w", err)
	}
	return object, w.cleanup()
}

// Abort is idempotent, including after a successful Commit.
func (w *Writer) Abort() (resultErr error) {
	if w.done {
		return nil
	}
	lock, err := w.store.lock()
	if err != nil {
		return errors.Join(err, w.closeFiles())
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	return w.cleanup()
}

func (w *Writer) closeFiles() error {
	w.done = true
	var result error
	if w.file != nil {
		result = w.file.Close()
		w.file = nil
	}
	result = errors.Join(result, w.lease.Close())
	w.lease = nil
	return result
}

func (w *Writer) cleanup() error {
	result := w.closeFiles()
	if err := w.store.removePrivateFile(w.dataName); err != nil {
		return errors.Join(result, err)
	}
	return errors.Join(result, w.store.removePrivateFile(w.leaseName))
}

// MoveTo preserves a written prefix and its hash state when staging must change filesystems.
// The original writer becomes read-only and must still be aborted by its owner.
func (w *Writer) MoveTo(destination *Store) (replacement *Writer, resultErr error) {
	if w.done || w.detached || w.file == nil || destination == w.store {
		return nil, fmt.Errorf("cas writer cannot move to that store")
	}
	replacement, err := destination.newWriter(w.hash)
	if err != nil {
		return nil, err
	}
	staged := replacement
	complete := false
	defer func() {
		if !complete {
			resultErr = errors.Join(resultErr, staged.Abort())
		}
	}()
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	count, err := io.Copy(replacement.file, w.file)
	if err != nil || count != w.size {
		return nil, errors.Join(fmt.Errorf("cannot preserve cas stage prefix"), err)
	}
	var digest Digest
	w.hash.Sum(digest[:0])
	w.object = Object{Digest: digest, Size: w.size}
	w.sealed = true
	w.detached = true
	replacement.size = w.size
	complete = true
	return replacement, nil
}

// DetachHash transfers a partial stream's hash state for a consumer's memory-only fallback.
// The original writer can only be aborted after this call.
func (w *Writer) DetachHash() (hash.Hash, error) {
	if w.done || w.detached {
		return nil, os.ErrClosed
	}
	w.sealed = true
	w.detached = true
	return w.hash, nil
}
