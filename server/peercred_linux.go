//go:build linux

package server

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// getPeerCredentials reads the connected peer's UID, GID and PID from Linux
// SO_PEERCRED, rejecting non-Unix connections or unavailable credentials.
func getPeerCredentials(conn net.Conn) (PeerCredentials, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return PeerCredentials{}, fmt.Errorf("connection is not a Unix socket")
	}
	rawConn, err := unixConn.SyscallConn()
	if err != nil {
		return PeerCredentials{}, err
	}

	var cred *unix.Ucred
	var controlErr error
	if err := rawConn.Control(func(fd uintptr) {
		cred, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return PeerCredentials{}, err
	}
	if controlErr != nil {
		return PeerCredentials{}, controlErr
	}
	if cred == nil {
		return PeerCredentials{}, fmt.Errorf("peer credentials unavailable")
	}
	return PeerCredentials{
		UID: cred.Uid,
		GID: cred.Gid,
		PID: int(cred.Pid),
	}, nil
}
