package bootstrap

import (
	"archive/zip"
	"fmt"
	"io"
)

// maxZipEntrySize bounds metadata entries read from a server jar so a zip bomb
// cannot exhaust memory.
const maxZipEntrySize = 10 << 20

// readZipEntry reads one named entry from an opened archive.
func readZipEntry(z *zip.Reader, name string) ([]byte, error) {
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer r.Close()
		lr := io.LimitReader(r, maxZipEntrySize+1)
		data, err := io.ReadAll(lr)
		if err != nil {
			return nil, err
		}
		if len(data) > maxZipEntrySize {
			return nil, fmt.Errorf("zip entry %s exceeds maximum allowed size (%d bytes)", name, maxZipEntrySize)
		}
		return data, nil
	}
	return nil, fmt.Errorf("missing zip entry %s", name)
}
