// Package buildid identifies the running Lucy binary by its content.
package buildid

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sync"
)

// Identity returns the SHA-256 digest of the running executable. Outputs that
// any change to Lucy's code can alter, such as bootstrap results, key on it,
// so a rebuilt binary never reuses output an older algorithm produced.
var Identity = sync.OnceValues(func() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the running executable: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read the running executable: %w", err)
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("digest the running executable: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
})
