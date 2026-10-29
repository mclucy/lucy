// Package artifactstore owns the durable per-user store for installed
// artifacts. It is separate from the network cache: entries survive cache
// expiration and clearing, and each registered workspace's lock retains the
// objects its installation pins.
package artifactstore

import (
	"fmt"
	"path/filepath"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/serverregistry"
)

// DirectoryName is the artifact store's directory inside Lucy's per-user data
// root.
const DirectoryName = "artifacts"

// Directory reports the per-user artifact store directory without creating it.
func Directory() (string, error) {
	root, err := serverregistry.DataRoot()
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
// object and staging namespaces as the per-user store.
func OpenAt(directory string) (*Store, error) {
	store, err := cas.New(directory)
	if err != nil {
		return nil, fmt.Errorf("open artifact store: %w", err)
	}
	return &Store{Store: store, directory: directory}, nil
}

// Store is the artifact store's content-addressed storage. Object names never
// carry a consumer's filename or extension; consumers give meaning to a digest
// through their locks.
type Store struct {
	*cas.Store
	directory string
}
