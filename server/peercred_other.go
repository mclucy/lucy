//go:build !linux && !darwin

package server

import (
	"fmt"
	"net"
)

// getPeerCredentials rejects platforms without an implemented peer-identity
// query so callers cannot authorize an unauthenticated connection.
func getPeerCredentials(_ net.Conn) (PeerCredentials, error) {
	return PeerCredentials{}, fmt.Errorf("peer credentials are not supported on this platform")
}
