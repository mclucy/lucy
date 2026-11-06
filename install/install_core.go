package install

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/log"
)

// removeSupersededPaperCore deletes the upstream paperclip jar after the
// installer has materialized the canonical server.jar. Leaving both files in
// the workspace makes discovery report two identical runtimes.
func removeSupersededPaperCore(root string, d *document.Lock, logger *log.Logger) {
	if d == nil {
		return
	}
	filename, superseded := bootstrap.SupersededCore(d.Server)
	if !superseded {
		return
	}
	expected := strings.ToLower(d.Server.Artifact.Hashes["sha256"])
	if expected == "" {
		return
	}
	directory := d.Server.Directory
	if directory == "" {
		directory = "."
	}
	path := filepath.Join(root, directory, filename)
	if _, err := os.Stat(path); err != nil {
		return
	}
	hashes, err := DigestFile(path, map[string]string{"sha256": expected})
	if err != nil {
		logger.Warn(err)
		return
	}
	if !strings.EqualFold(hashes["sha256"], expected) {
		return
	}
	if err := os.Remove(path); err != nil {
		logger.Warn(err)
		return
	}
	logger.Debug("removed superseded core", "path", path)
}
