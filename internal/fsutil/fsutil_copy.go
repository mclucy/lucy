package fsutil

import (
	"io"
	"os"
	"path/filepath"
)

// AtomicCopyFile writes src to dst with mode, replacing dst through a
// temporary file beside dst. An interrupted copy never leaves a partial file,
// and other links to the previous dst keep their bytes.
func AtomicCopyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*")
	if err != nil {
		return err
	}
	temporary := out.Name()
	defer func() {
		if out != nil {
			out.Close()
			os.Remove(temporary)
		}
	}()
	if err := out.Chmod(mode); err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	out = nil
	return os.Rename(temporary, dst)
}
