//go:build !debug

package server

func serviceDryRunEnabled() bool {
	return false
}
