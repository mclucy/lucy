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

// authorizeIPCServer authenticates the endpoint before sending request data.
// A stale or preclaimed socket must not impersonate lucyd or another run user.
func authorizeIPCServer(conn net.Conn, socketPath string, req Request) error {
	cred, err := getPeerCredentials(conn)
	if err != nil {
		return fmt.Errorf("authorize IPC server: %w", err)
	}
	if cred.UID == 0 || (serviceDryRunEnabled() && os.Geteuid() != 0 && cred.UID == uint32(os.Geteuid())) {
		return nil
	}
	if socketPath != DaemonSocketPath() && socketPath == RunnerSocketPath(req.Instance) {
		inst, err := requiredInstance(req.Instance)
		if err != nil {
			return err
		}
		uid, err := uidForUser(inst.RunUser)
		if err != nil {
			return err
		}
		if cred.UID == uid {
			return nil
		}
	}
	return fmt.Errorf("permission denied: unexpected IPC server uid %d", cred.UID)
}

// authorizeDaemonRequest checks kernel-reported peer credentials before dispatch;
// runner registration additionally requires ownership of the registered instance.
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

// daemonPeerIsAllowed permits root and Lucy administrators, plus the current user
// when a non-root debug daemon is running in service dry-run mode.
func daemonPeerIsAllowed(cred PeerCredentials) bool {
	if cred.UID == 0 {
		return true
	}
	if serviceDryRunEnabled() && os.Geteuid() != 0 && cred.UID == uint32(os.Geteuid()) {
		return true
	}
	return uidInGroup(cred.UID, cred.GID, DefaultGroup)
}

// authorizeRunnerRegistration requires the canonical instance socket and root or
// its configured run user; debug dry-run also permits the current user.
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

// uidInGroup checks primary and supplementary membership, denying access if the
// system cannot resolve the requested group or the user's memberships.
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

// uidForUser resolves a system account and rejects UIDs outside the credential range.
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
