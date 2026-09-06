package server

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
)

// ensureSharedDir creates a real directory and applies ownership and permissions.
// Non-owners may reuse an already configured shared directory without chmod.
func ensureSharedDir(path string, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("runtime path must be a directory, not a symlink: %s", path)
	}
	if os.Geteuid() != 0 && !directoryOwnedBy(info, os.Geteuid()) {
		if info.Mode()&(os.ModePerm|os.ModeSticky|os.ModeSetgid) != mode {
			return fmt.Errorf("unexpected permissions on shared directory %s", path)
		}
		return nil
	}
	if err := chownGroup(path, DefaultGroup); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

// chownGroup makes a privileged runtime path root-owned with the selected group.
// Unprivileged runners retain their own ownership and cannot change groups here.
// Debug dry-runs enforce root ownership without requiring a system-wide Lucy group.
func chownGroup(path, groupName string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	if serviceDryRunEnabled() {
		return os.Chown(path, 0, -1)
	}
	group, err := user.LookupGroup(groupName)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return err
	}
	return os.Chown(path, 0, gid)
}

// prepareRuntimeDirs secures the daemon parent and a sticky, root-owned runner
// directory. The latter lets launchd's unprivileged runners create only their
// own sockets without allowing another group member to unlink live endpoints.
func prepareRuntimeDirs() error {
	owner := 0
	if serviceDryRunEnabled() && os.Geteuid() != 0 {
		owner = os.Geteuid()
	}
	for _, dir := range []struct {
		path string
		mode os.FileMode
	}{
		{RunDir(), 0o755},
		{RunnerSocketDir(), os.ModeSticky | 0o775},
	} {
		if os.Geteuid() == owner {
			if err := ensureSharedDir(dir.path, dir.mode); err != nil {
				return fmt.Errorf("prepare runtime directory %s: %w", dir.path, err)
			}
		}
		info, err := os.Lstat(dir.path)
		if err != nil {
			return fmt.Errorf("read runtime directory %s (start lucyd first): %w", dir.path, err)
		}
		if !info.IsDir() || !directoryOwnedBy(info, owner) || info.Mode()&(os.ModePerm|os.ModeSticky) != dir.mode {
			return fmt.Errorf("unsafe ownership or permissions on runtime directory %s; start lucyd to repair it", dir.path)
		}
	}
	return nil
}
