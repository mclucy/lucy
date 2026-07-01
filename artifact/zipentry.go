package artifact

import (
	"archive/zip"

	"github.com/mclucy/lucy/internal/fsutil"
)

// ReadZipEntryBytes returns the contents of a named ZIP entry, or nil if missing.
func ReadZipEntryBytes(zipRdr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zipRdr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := fsutil.CopyBytes(rc, fsutil.MaxZipEntryBytes)
		closeErr := rc.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return data, nil
	}
	return nil, nil
}

func readZipEntry(zipRdr *zip.Reader, name string) ([]byte, error) {
	return ReadZipEntryBytes(zipRdr, name)
}
