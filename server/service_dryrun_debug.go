//go:build debug

package server

import "os"

func serviceDryRunEnabled() bool {
	return os.Getenv("LUCY_SERVICE_DRY_RUN") != ""
}
