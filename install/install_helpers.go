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
	case types.EcoAny:
		return nil
	case types.EcoMcdr:
		if ws.Environments.Mcdr == nil {
			return errors.New("mcdr not found")
		}
		return nil
	default:
		if !ws.Server.IsValid() {
			return errors.New("no valid executable found, `lucy add` requires a server in current directory")
		}

		switch platform {
		case types.EcoVelocity, types.EcoBungeecord, types.EcoSponge:
			return nil
		}

		result := workspace.EvaluateCompatibility(ws.Server, platform)
		switch result.Verdict {
		case types.CompatCompatible:
			return nil
		case types.CompatDegraded:
			log.ShowWarn(
				fmt.Errorf(
					"compatibility degraded for %s: %s (reason: %s)",
					platform,
					result.Detail,
					result.Reason,
				),
			)
			return nil
		case types.CompatUnresolved:
			return fmt.Errorf(
				"topology unresolved for %s: cannot determine server compatibility",
				platform.Title(),
			)
		case types.CompatIncompatible:
			return fmt.Errorf(
				"%s packages are incompatible with the current runtime (reason: %s, verdict: %s)",
				platform.Title(),
				result.Reason,
				result.Verdict,
			)
		default:
			return fmt.Errorf(
				"%s runtime compatibility could not be confirmed (reason: %s, verdict: %s)",
				platform.Title(),
				result.Reason,
				result.Verdict,
			)
		}
	}
}
