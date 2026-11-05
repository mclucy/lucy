package bootstrap

import (
	"path/filepath"

	"github.com/mclucy/lucy/internal/fsutil"
)

// preparePlain copies the locked core into the server tree unchanged.
func preparePlain(root, corePath string) error {
	return fsutil.AtomicCopyFile(corePath, filepath.Join(root, "server.jar"), 0o644)
}
