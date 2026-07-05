//go:build unix || darwin || linux

package server

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

func applyRunUser(cmd *exec.Cmd, runUser string) error {
	if runUser == "" || os.Geteuid() != 0 {
		return nil
	}
	u, err := user.Lookup(runUser)
	if err != nil {
		return fmt.Errorf("lookup run user %q: %w", runUser, err)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse uid for %q: %w", runUser, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse gid for %q: %w", runUser, err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
	}
	return nil
}
