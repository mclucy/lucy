//go:build !linux && !darwin

package server

import (
	"fmt"
	"net"
)

func getPeerCredentials(_ net.Conn) (PeerCredentials, error) {
	return PeerCredentials{}, fmt.Errorf("peer credentials are not supported on this platform")
}
