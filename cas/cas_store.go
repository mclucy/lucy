package cas

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/fsutil"
)

var (
	ErrCorrupt = errors.New("cas contents do not match their digest")
	ErrRead    = errors.New("cannot read cas contents")
)

// Store has one private filesystem root and no consumer metadata.
// Close it after its writers finish; open readers own their file handles.
type Store struct {
	root      *os.Root
	directory string
}

// New rejects a symlink store root and creates private object and staging namespaces.
func New(path string) (*Store, error) {
	if err := fsutil.MkdirAllPrivate(path); err != nil {
		return nil, fmt.Errorf("create cas root: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.Join(fmt.Errorf("cas root must be a regular directory"), err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("open cas root: %w", err)
	}
	for _, name := range []string{"objects", "objects/sha256", "tmp"} {
		if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, errors.Join(err, root.Close())
		}
		info, err := root.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.Join(fmt.Errorf("invalid cas namespace %s", name), err, root.Close())
		}
	}
	return &Store{root: root, directory: path}, nil
}

func (s *Store) Close() error {
	return s.root.Close()
}

// Path reports the filesystem path of a published object. It does not check
// that the object exists; callers open it through their own filesystem view.
func (s *Store) Path(digest Digest) string {
	return filepath.Join(s.directory, filepath.FromSlash(objectPath(digest)))
}

func (s *Store) lock() (*fsutil.FileLock, error) {
	if info, err := s.root.Lstat("cas.lock"); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("cas lock must be a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := s.root.OpenFile("cas.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	return fsutil.LockOpenedFile(file, true)
}

func objectPath(digest Digest) string {
	hex := digest.Hex()
	return "objects/sha256/" + hex[:2] + "/" + hex
}

// Open verifies the digest as the stream reaches EOF. Discard copied bytes on error.
func (s *Store) Open(digest Digest) (io.ReadCloser, error) {
	name := objectPath(digest)
	info, err := s.root.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("open cas object: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: object is not a regular file", ErrCorrupt)
	}
	file, err := s.root.Open(name)
	if err != nil {
		return nil, errors.Join(ErrRead, err)
	}
	return &verifiedReader{
		file:     file,
		expected: digest,
		hash:     sha256.New(),
	}, nil
}

// Put computes the address while writing and never publishes a failed stream.
func (s *Store) Put(reader io.Reader, expected *Digest) (object Object, resultErr error) {
	writer, err := s.NewWriter()
	if err != nil {
		return object, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Abort()) }()
	if _, err := io.Copy(writer, reader); err != nil {
		return object, fmt.Errorf("ingest cas contents: %w", err)
	}
	return writer.Commit(expected)
}

// Walk visits physical objects once. Consumers must coordinate retention with their metadata.
func (s *Store) Walk(visit func(Object) error) error {
	return fs.WalkDir(s.root.FS(), "objects/sha256", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		digest, err := ParseDigest("sha256:" + filepath.Base(name))
		if err != nil || filepath.ToSlash(name) != objectPath(digest) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return visit(Object{Digest: digest, Size: info.Size()})
	})
}

// Remove is for consumer-approved reclamation, not automatic retention.
// Readers already opened on systems that allow to unlink can finish reading their handles.
func (s *Store) Remove(digest Digest) (size int64, resultErr error) {
	lock, err := s.lock()
	if err != nil {
		return 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	return s.removeLocked(digest)
}

func (s *Store) removeLocked(digest Digest) (int64, error) {
	name := objectPath(digest)
	info, err := s.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("refuse to remove non-regular cas object")
	}
	garbage := "tmp/gc-" + rand.Text()
	if err := fsutil.RenameRoot(s.root, name, garbage); err != nil {
		return 0, err
	}
	if err := s.removePrivateFile(garbage); err != nil {
		return 0, fmt.Errorf("remove reclaimed cas contents: %w", err)
	}
	return info.Size(), nil
}

// Sweep holds the store lock while plan determines the retained digests and
// locks the state those references came from, then removes every other object.
// No writer can publish from the moment the lock is acquired through the last
// removal, so an object published by an operation that started after plan has
// not run cannot be collected. plan may return a release function for state it
// locked; Sweep runs it before releasing the store lock. A nil reference set is
// rejected rather than read as "delete all", and a plan or enumeration error
// removes nothing.
func (s *Store) Sweep(plan func() (map[Digest]struct{}, func() error, error)) (removed int, bytes int64, resultErr error) {
	if plan == nil {
		return 0, 0, fmt.Errorf("sweep requires a plan")
	}
	lock, err := s.lock()
	if err != nil {
		return 0, 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	referenced, release, err := plan()
	if release != nil {
		defer func() { resultErr = errors.Join(resultErr, release()) }()
	}
	if err != nil {
		return 0, 0, err
	}
	if referenced == nil {
		return 0, 0, fmt.Errorf("sweep requires a reference set")
	}
	var objects []Object
	if err := s.Walk(func(object Object) error {
		objects = append(objects, object)
		return nil
	}); err != nil {
		return 0, 0, err
	}
	var failures []error
	for _, object := range objects {
		if _, kept := referenced[object.Digest]; kept {
			continue
		}
		size, err := s.removeLocked(object.Digest)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		removed++
		bytes += size
	}
	return removed, bytes, errors.Join(failures...)
}

// Cleanup removes only stages whose lease is no longer held by a writer.
func (s *Store) Cleanup() (resultErr error) {
	lock, err := s.lock()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	directory, err := s.root.Open("tmp")
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(-1)
	if closeErr := directory.Close(); err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "gc-") && entry.Type().IsRegular() {
			if err := s.removePrivateFile("tmp/" + entry.Name()); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(entry.Name(), "stage-") && strings.HasSuffix(entry.Name(), ".data") {
			dataName := "tmp/" + entry.Name()
			leaseName := strings.TrimSuffix(dataName, ".data") + ".lease"
			if _, err := s.root.Lstat(leaseName); errors.Is(err, os.ErrNotExist) {
				if err := s.removePrivateFile(dataName); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			continue
		}
		if !strings.HasPrefix(entry.Name(), "stage-") || !strings.HasSuffix(entry.Name(), ".lease") {
			continue
		}
		if !entry.Type().IsRegular() {
			continue
		}
		leaseName := "tmp/" + entry.Name()
		file, err := s.root.OpenFile(leaseName, os.O_RDWR, 0)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		lease, err := fsutil.LockOpenedFile(file, false)
		if errors.Is(err, fsutil.ErrLocked) {
			continue
		}
		if err != nil {
			return err
		}
		if err := lease.Close(); err != nil {
			return err
		}
		dataName := strings.TrimSuffix(leaseName, ".lease") + ".data"
		for _, name := range []string{dataName, leaseName} {
			if err := s.removePrivateFile(name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) removePrivateFile(name string) error {
	info, err := s.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refuse non-regular cas cleanup path")
	}
	if err := s.root.Chmod(name, 0o600); err != nil {
		return err
	}
	return s.root.Remove(name)
}
