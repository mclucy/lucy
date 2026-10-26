package fsutil

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
)

// MaxZipEntryBytes is the upper bound for reading a single entry from a JAR/ZIP
// when the full payload is required for parsing (metadata descriptors).
const MaxZipEntryBytes = 32 * 1024 * 1024

// MaxNestedZipEntryBytes is the upper bound for reading a ZIP entry that
// embeds another archive, such as a nested JAR or an installer payload.
const MaxNestedZipEntryBytes = 128 * 1024 * 1024

// ReadZipEntry returns the contents of the named entry. found reports whether
// the archive carries that entry. An entry declared larger than maxBytes fails
// before decompression starts; maxBytes <= 0 disables the bound.
func ReadZipEntry(
	z *zip.Reader,
	name string,
	maxBytes int64,
) (data []byte, found bool, err error) {
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		if maxBytes > 0 && f.UncompressedSize64 > uint64(maxBytes) {
			return nil, true, fmt.Errorf(
				"zip entry %s exceeds %d bytes",
				name,
				maxBytes,
			)
		}
		r, err := f.Open()
		if err != nil {
			return nil, true, err
		}
		data, readErr := CopyBytes(r, maxBytes)
		return data, true, errors.Join(readErr, r.Close())
	}
	return nil, false, nil
}

// CopyBytes reads r into memory. When maxBytes > 0, r is wrapped in
// io.LimitReader(max+1) so reads fail if the stream exceeds the limit.
func CopyBytes(r io.Reader, maxBytes int64) ([]byte, error) {
	limited := r
	if maxBytes > 0 {
		limited = io.LimitReader(r, maxBytes+1)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, limited); err != nil {
		return nil, err
	}

	if maxBytes > 0 && int64(buf.Len()) > maxBytes {
		return nil, fmt.Errorf("read exceeded %d bytes", maxBytes)
	}
	return buf.Bytes(), nil
}
