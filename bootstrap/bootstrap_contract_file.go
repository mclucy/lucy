package bootstrap

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
)

// readBoundedFile reads at most maxBytes from path so a hostile or damaged
// launch file cannot exhaust memory.
func readBoundedFile(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	lr := io.LimitReader(f, maxBytes+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file %s exceeds maximum allowed size (%d bytes)", path, maxBytes)
	}
	return data, nil
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// scopedPath joins relPath under scope and rejects any path that would leave
// that bounded directory.
func scopedPath(scope, relPath string) (string, error) {
	cleanRel := filepath.Clean(relPath)
	for _, part := range strings.Split(filepath.ToSlash(cleanRel), "/") {
		if part == ".." {
			return "", fmt.Errorf("path %q contains illegal parent directory traversal", relPath)
		}
	}
	if err := document.SafeRelativePath(cleanRel); err != nil {
		return "", fmt.Errorf("unsafe relative path %q: %w", relPath, err)
	}
	cleanScope := filepath.ToSlash(filepath.Clean(scope))
	if !strings.HasSuffix(cleanScope, "/") {
		cleanScope += "/"
	}
	joined := filepath.ToSlash(filepath.Clean(filepath.Join(cleanScope, cleanRel)))
	if !strings.HasPrefix(joined, cleanScope) {
		return "", fmt.Errorf("path %q escapes declared scope %q", relPath, scope)
	}
	return joined, nil
}
