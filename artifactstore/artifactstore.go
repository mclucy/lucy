// Package artifactstore owns the durable per-user store for installed
// artifacts. It is separate from the network cache: entries survive cache
// expiration and clearing, and each registered workspace's lock retains the
// objects its installation pins.
package artifactstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/internal/appdata"
	"github.com/mclucy/lucy/internal/fsutil"
)

const (
	// DirectoryName is the artifact store's directory inside Lucy's per-user
	// data root.
	DirectoryName = "artifacts"

	operationsDirectoryName = "operations"
)

// Directory reports the per-user artifact store directory without creating it.
func Directory() (string, error) {
	root, err := appdata.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, DirectoryName), nil
}

// Open opens the per-user store, creating its private namespaces when absent.
func Open() (*Store, error) {
	directory, err := Directory()
	if err != nil {
		return nil, fmt.Errorf("locate artifact store: %w", err)
	}
	return OpenAt(directory)
}

// OpenAt opens a store at an explicit directory. It creates the same private
// object, staging, and operation namespaces as the per-user store.
func OpenAt(directory string) (*Store, error) {
	store, err := cas.New(directory)
	if err != nil {
		return nil, fmt.Errorf("open artifact store: %w", err)
	}
	operations := filepath.Join(directory, operationsDirectoryName)
	if err := createOperations(operations); err != nil {
		return nil, errors.Join(fmt.Errorf("open artifact store operations: %w", err), store.Close())
	}
	return &Store{Store: store, operations: operations}, nil
}

// Store is the artifact store's content-addressed storage. Object names never
// carry a consumer's filename or extension; consumers give meaning to a digest
// through their locks.
type Store struct {
	*cas.Store
	operations string
}

// createOperations creates the private directory that holds the operation gate
// and leases, separate from the content store's staging namespace.
func createOperations(directory string) error {
	if err := fsutil.MkdirAllPrivate(directory); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("operations namespace must be a regular directory")
	}
	return nil
}
