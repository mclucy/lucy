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
		if ws.Server() == nil {
			return errors.New(
				"no valid executable found, `lucy add` requires a server in current directory",
			)
		}

		offered, level, ok := ws.Supports(id)
		if !ok {
			return fmt.Errorf(
				"%s packages are incompatible with the current runtime",
				platform.Title(),
			)
		}
		if level == types.CompatDegraded {
			log.ShowWarn(fmt.Errorf(
				"%s support is degraded through %s compatibility",
				platform.Title(),
				offered.Title(),
			))
		}
		return nil
	}
}
