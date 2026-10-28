package operation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/workspacewrite"
)

// RenameServer changes only the manifest name of the workspace rooted at root.
// It returns the name the manifest carried before the change. Every other
// declaration, the lockfile, and installed server content stay untouched, so
// no dependency or runtime work follows a rename.
func (s *Service) RenameServer(root string, name string) (previous string, resultErr error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	name, err = document.NormalizeName(name)
	if err != nil {
		return "", err
	}
	if err := workspacewrite.SafePath(root, document.ManifestFilename); err != nil {
		return "", err
	}
	writer, err := workspacewrite.Lock(root, s.logger)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := workspacewrite.RecoverInstall(root, s.logger); err != nil {
		return "", err
	}
	pending, err := filetrack.HasPendingTransaction(root)
	if err != nil {
		return "", err
	}
	if pending {
		if err := filetrack.RecoverPending(root); err != nil {
			return "", err
		}
	}
	manifest, err := document.ManifestFile(root).Read()
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no lucy workspace found in %s", root)
	}
	if err != nil {
		return "", err
	}
	previous = manifest.Name
	if previous == name {
		s.logger.Debug("server name unchanged", "root", root, "name", name)
		return previous, nil
	}
	manifest.Name = name
	if err := document.ManifestFile(root).Write(manifest); err != nil {
		return "", err
	}
	s.logger.Info("server renamed", "root", root, "previous", previous, "name", name)
	return previous, nil
}
