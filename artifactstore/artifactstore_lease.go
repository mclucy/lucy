package artifactstore

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mclucy/lucy/internal/fsutil"
)

const (
	operationsLockName = "operations.lock"
	operationPrefix    = "operation-"
	operationSuffix    = ".lease"
)

// Lease marks one in-flight operation that may acquire artifacts before it has
// registered a workspace. It is global to the per-user store and survives
// process boundaries through a private file lock. Hold it from before the
// first artifact access through registration, installation, or planning so a
// collection sweep cannot reclaim bytes the operation is still using.
type Lease struct {
	file      *os.File
	lock      *fsutil.FileLock
	name      string
	closeOnce sync.Once
	closeErr  error
}

// AcquireOperation acquires a lease on the per-user artifact store.
func AcquireOperation() (*Lease, error) {
	store, err := Open()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.AcquireLease()
}

// AcquireLease creates and locks a unique operation lease. Lease creation is
// serialized by the same gate a collection uses, so a sweep either observes
// the lease before deleting or waits for the lease to exist.
func (s *Store) AcquireLease() (*Lease, error) {
	gate, err := fsutil.LockFile(s.tmpPath(operationsLockName))
	if err != nil {
		return nil, fmt.Errorf("lock artifact store operations: %w", err)
	}
	defer func() { _ = gate.Close() }()
	for {
		name := s.tmpPath(operationPrefix + rand.Text() + operationSuffix)
		file, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("create artifact store lease: %w", err)
		}
		lock, err := fsutil.LockOpenedFile(file, false)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("lock artifact store lease: %w", err),
				os.Remove(name),
			)
		}
		return &Lease{file: file, lock: lock, name: name}, nil
	}
}

// Close releases the lease and removes its file. It is idempotent.
func (l *Lease) Close() error {
	if l == nil {
		return nil
	}
	l.closeOnce.Do(func() {
		var result error
		if l.lock != nil {
			result = l.lock.Close()
		}
		if l.file != nil && l.lock == nil {
			result = errors.Join(result, l.file.Close())
		}
		if err := os.Remove(l.name); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
		l.closeErr = result
	})
	return l.closeErr
}

// beginCollection takes the operation gate and refuses when any lease is
// active. Stale lease files left by a crashed process are removed. The gate
// must stay held through the sweep so no operation can acquire artifacts
// between this check and the last removal.
func (s *Store) beginCollection() (*fsutil.FileLock, error) {
	gate, err := fsutil.LockFile(s.tmpPath(operationsLockName))
	if err != nil {
		return nil, fmt.Errorf("lock artifact store operations: %w", err)
	}
	entries, err := os.ReadDir(s.tmpPath())
	if err != nil {
		return nil, errors.Join(err, gate.Close())
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, operationPrefix) || !strings.HasSuffix(name, operationSuffix) {
			continue
		}
		path := s.tmpPath(name)
		if !entry.Type().IsRegular() {
			return nil, errors.Join(
				fmt.Errorf("artifact store operation lease %s is not a regular file", path),
				gate.Close(),
			)
		}
		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, errors.Join(err, gate.Close())
		}
		lock, lockErr := fsutil.LockOpenedFile(file, false)
		if errors.Is(lockErr, fsutil.ErrLocked) {
			return nil, errors.Join(
				errors.New("another Lucy operation is acquiring or using artifacts"),
				gate.Close(),
			)
		}
		if lockErr != nil {
			return nil, errors.Join(lockErr, gate.Close())
		}
		closeErr := lock.Close()
		removeErr := os.Remove(path)
		if closeErr != nil || removeErr != nil {
			return nil, errors.Join(closeErr, removeErr, gate.Close())
		}
	}
	return gate, nil
}

func (s *Store) tmpPath(parts ...string) string {
	joined := make([]string, 0, len(parts)+2)
	joined = append(joined, s.directory, "tmp")
	joined = append(joined, parts...)
	return filepath.Join(joined...)
}
