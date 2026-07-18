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

		admission := workspace.EvaluateAdmission(ws.Server, platform)
		switch admission.Verdict {
		case workspace.AdmissionDirect:
			return nil
		case workspace.AdmissionDegraded:
			log.ShowWarn(fmt.Errorf(
				"%s package admission is degraded through %s compatibility",
				platform.Title(),
				admission.Offered.Title(),
			))
			return nil
		case workspace.AdmissionUnresolved:
			return fmt.Errorf(
				"runtime unavailable: cannot determine %s package admission",
				platform.Title(),
			)
		case workspace.AdmissionRejected:
			return fmt.Errorf(
				"%s packages are incompatible with the current runtime",
				platform.Title(),
			)
		default:
			return fmt.Errorf(
				"%s package admission could not be determined",
				platform.Title(),
			)
		}
	}
}
