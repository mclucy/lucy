package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
)

// checkPathSafety rejects symlinks, devices, and non-directory components
// below root. It walks each segment of rel, so an intermediate symlink cannot
// redirect later reads outside the tree. A missing segment ends the check;
// the caller verifies the file it expects separately.
func checkPathSafety(root, rel string, checkLeaf bool) error {
	if rel != "" {
		if err := document.SafeRelativePath(rel); err != nil {
			return fmt.Errorf("unsafe relative path %q: %w", rel, err)
		}
	}

	cleanRoot := filepath.Clean(root)
	rootInfo, err := os.Lstat(cleanRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink detected in root path: %s", cleanRoot)
	}
	if rootInfo.Mode()&(os.ModeNamedPipe|os.ModeSocket|os.ModeDevice) != 0 {
		return fmt.Errorf("unsupported file type in root path: %s (%v)", cleanRoot, rootInfo.Mode())
	}

	if rel == "" || rel == "." {
		return nil
	}

	cleanRel := filepath.Clean(rel)
	parts := strings.Split(filepath.ToSlash(cleanRel), "/")
	cur := cleanRoot
	for i, part := range parts {
		if part == "." || part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink detected in path segment %s", cur)
		}
		if info.Mode()&(os.ModeNamedPipe|os.ModeSocket|os.ModeDevice) != 0 {
			return fmt.Errorf("unsupported file type in path %s (%v)", cur, info.Mode())
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("path component is not a directory: %s", cur)
		}
		if i == len(parts)-1 && checkLeaf {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("leaf path is not a regular file: %s (%v)", cur, info.Mode())
			}
		}
	}
	return nil
}
