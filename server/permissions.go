package server

import (
	"os"
	"os/user"
	"strconv"
)

func ensureSharedDir(path string, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	_ = os.Chmod(path, mode)
	_ = chownGroup(path, DefaultGroup)
	return nil
}

func chownGroup(path, groupName string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	group, err := user.LookupGroup(groupName)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return err
	}
	return os.Chown(path, -1, gid)
}
