//go:build debug

package server

import "os"

// serviceDryRunEnabled allows debug builds to simulate service commands when
// LUCY_SERVICE_DRY_RUN is nonempty.
func serviceDryRunEnabled() bool {
	return os.Getenv("LUCY_SERVICE_DRY_RUN") != ""
}
