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

// applyRunUser configures a root-launched child with the target account's UID,
// primary GID and supplementary groups. Non-root callers retain their credentials.
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
	groupIDs, err := u.GroupIds()
	if err != nil {
		return fmt.Errorf("lookup groups for %q: %w", runUser, err)
	}
	groups := make([]uint32, 0, len(groupIDs))
	for _, id := range groupIDs {
		groupID, err := strconv.ParseUint(id, 10, 32)
		if err != nil {
			return fmt.Errorf("parse supplementary group for %q: %w", runUser, err)
		}
		groups = append(groups, uint32(groupID))
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Credential = &syscall.Credential{
		Uid:    uint32(uid),
		Gid:    uint32(gid),
		Groups: groups,
	}
	return nil
}
