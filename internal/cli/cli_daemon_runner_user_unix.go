//go:build unix || darwin || linux

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
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
	// In particular, package-task Lucy children must not try to open root's
	// default log/config directories after credentials have been dropped.
	env := cmd.Environ()
	filtered := env[:0]
	for _, value := range env {
		key, _, _ := strings.Cut(value, "=")
		switch key {
		case "HOME", "USER", "LOGNAME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME":
			continue
		}
		filtered = append(filtered, value)
	}
	cmd.Env = append(filtered, "HOME="+u.HomeDir, "USER="+u.Username, "LOGNAME="+u.Username)
	return nil
}
