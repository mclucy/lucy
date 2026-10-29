package install

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/log"
)

// removeSupersededPaperCore deletes the adopted paperclip jar after the
// installer has materialized the canonical server.jar. Leaving both files in
// the workspace makes discovery report two identical runtimes.
func removeSupersededPaperCore(root string, d *document.Lock, logger *log.Logger) {
	if d == nil || d.Server.Bootstrapper != "paperclip" {
		return
	}
	filename := d.Server.Artifact.Filename
	if filename == "" || filename == "server.jar" {
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
	logger.Debug("removed superseded Paper core", "path", path)
}
