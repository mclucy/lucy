package install

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

func ensureServerEcosystemMatch(
	id types.VersionedPackageRef,
	ws workspace.Workspace,
) error {
	platform := id.Eco

	switch platform {
	case types.EcoUnspecified:
		return nil
	case types.EcoMcdr:
		if ws.Environments.Mcdr == nil {
			return errors.New("mcdr not found")
		}
		return nil
	default:
		if ws.Server == nil || !ws.Server.IsValid() {
			return errors.New(
				"no valid executable found, `lucy add` requires a server in current directory",
			)
		}

		level, offered := workspace.EvaluateRuntimeCompatibility(
			ws.Server,
			platform,
		)
		switch level {
		case types.CompatCompatible:
			return nil
		case types.CompatDegraded:
			log.ShowWarn(fmt.Errorf(
				"%s support is degraded through %s compatibility",
				platform.Title(),
				offered.Title(),
			))
			return nil
		case types.CompatUnknown:
			return fmt.Errorf(
				"runtime unavailable: cannot determine %s package compatibility",
				platform.Title(),
			)
		case types.CompatIncompatible:
			return fmt.Errorf(
				"%s packages are incompatible with the current runtime",
				platform.Title(),
			)
		default:
			return fmt.Errorf(
				"%s package compatibility could not be determined",
				platform.Title(),
			)
		}
	}
}
