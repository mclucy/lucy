//go:build darwin

package server

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func getPeerCredentials(conn net.Conn) (PeerCredentials, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return PeerCredentials{}, fmt.Errorf("connection is not a Unix socket")
	}
	rawConn, err := unixConn.SyscallConn()
	if err != nil {
		return PeerCredentials{}, err
	}

	var cred *unix.Xucred
	var controlErr error
	if err := rawConn.Control(func(fd uintptr) {
		cred, controlErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return PeerCredentials{}, err
	}
	if controlErr != nil {
		return PeerCredentials{}, controlErr
	}
	if cred == nil {
		return PeerCredentials{}, fmt.Errorf("peer credentials unavailable")
	}

	var gid uint32
	if cred.Ngroups > 0 {
		gid = cred.Groups[0]
	}
	return PeerCredentials{
		UID: cred.Uid,
		GID: gid,
	}, nil
}
