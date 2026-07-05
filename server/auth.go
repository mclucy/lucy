package server

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

type PeerCredentials struct {
	UID uint32
	GID uint32
	PID int
}

func authorizeDaemonRequest(conn net.Conn, req Request) error {
	cred, err := getPeerCredentials(conn)
	if err != nil {
		return fmt.Errorf("authorize daemon request: %w", err)
	}

	if req.Op == OpRunnerRegister {
		return authorizeRunnerRegistration(cred, req.Runner)
	}

	if daemonPeerIsAllowed(cred) {
		return nil
	}
	return fmt.Errorf(
		"permission denied: daemon requests require root or membership in the %q group; add your user to %s or run with sudo/root",
		DefaultGroup,
		DefaultGroup,
	)
}

func daemonPeerIsAllowed(cred PeerCredentials) bool {
	if cred.UID == 0 {
		return true
	}
	if serviceDryRunEnabled() && os.Geteuid() != 0 && cred.UID == uint32(os.Geteuid()) {
		return true
	}
	return uidInGroup(cred.UID, cred.GID, DefaultGroup)
}

func authorizeRunnerRegistration(cred PeerCredentials, reg RunnerRegistration) error {
	if err := ValidateInstanceName(reg.Name); err != nil {
		return err
	}
	expectedSocket := filepath.Clean(RunnerSocketPath(reg.Name))
	if filepath.Clean(reg.SocketPath) != expectedSocket {
		return fmt.Errorf("runner registration for %q used unexpected socket path", reg.Name)
	}
	if cred.UID == 0 {
		return nil
	}
	if serviceDryRunEnabled() && os.Geteuid() != 0 && cred.UID == uint32(os.Geteuid()) {
		return nil
	}

	inst, err := requiredInstance(reg.Name)
	if err != nil {
		return err
	}
	runUID, err := uidForUser(inst.RunUser)
	if err != nil {
		return err
	}
	if cred.UID == runUID {
		return nil
	}
	return fmt.Errorf("permission denied: runner registration for %q must come from root or run_user %q", reg.Name, inst.RunUser)
}

func uidInGroup(uid, gid uint32, groupName string) bool {
	group, err := user.LookupGroup(groupName)
	if err != nil {
		return false
	}
	if strconv.FormatUint(uint64(gid), 10) == group.Gid {
		return true
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return false
	}
	groupIDs, err := u.GroupIds()
	if err != nil {
		return false
	}
	for _, groupID := range groupIDs {
		if groupID == group.Gid {
			return true
		}
	}
	return false
}

func uidForUser(name string) (uint32, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, fmt.Errorf("lookup user %q: %w", name, err)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse uid for %q: %w", name, err)
	}
	return uint32(uid), nil
}
