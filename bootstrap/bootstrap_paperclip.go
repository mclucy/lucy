package bootstrap

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"

	"github.com/mclucy/lucy/internal/fsutil"
)

// preparePaperclip copies the locked core and lets the Paperclip patcher
// materialize its patched server in place.
func preparePaperclip(
	ctx context.Context,
	root string,
	corePath string,
	java string,
	output io.Writer,
) error {
	if err := fsutil.AtomicCopyFile(corePath, filepath.Join(root, "server.jar"), 0o644); err != nil {
		return err
	}
	if err := requireJava(java, "Paperclip patching"); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, java, "-Dpaperclip.patchonly=true", "-jar", "server.jar")
	cmd.Dir = root
	captured, err := runLockedProcess(cmd, output)
	if err != nil {
		return fmt.Errorf("Paperclip patching: %w\n%s", err, captured)
	}
	return nil
}
