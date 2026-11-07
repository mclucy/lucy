package artifact

import (
	"archive/zip"

	"github.com/mclucy/lucy/internal/fsutil"
)

// readZipEntryBytes returns the contents of a named ZIP entry, or nil if missing.
func readZipEntryBytes(zipRdr *zip.Reader, name string) ([]byte, error) {
	data, _, err := fsutil.ReadZipEntry(
		zipRdr,
		name,
		fsutil.MaxZipEntryBytes,
	)
	return data, err
}
