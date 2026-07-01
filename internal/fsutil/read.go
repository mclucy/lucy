package fsutil

import (
	"bytes"
	"fmt"
	"io"
)

// MaxZipEntryBytes is the upper bound for reading a single entry from a JAR/ZIP
// when the full payload is required for parsing (metadata descriptors).
const MaxZipEntryBytes = 32 * 1024 * 1024

// CopyBytes reads from r into memory using io.Copy (streaming). When maxBytes > 0,
// r is wrapped with io.LimitReader(max+1) so
// reads fail if the stream exceeds the limit.
func CopyBytes(r io.Reader, maxBytes int64) ([]byte, error) {
	var limited io.Reader = r
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
