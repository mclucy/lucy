//go:build !debug

package cli

// serviceDryRunEnabled keeps service simulation disabled in non-debug builds,
// regardless of environment variables.
func serviceDryRunEnabled() bool {
	return false
}
