package install

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
)

func ensureServerPlatformMatch(id types.VersionedPackageRef) error {
	platform := id.Platform
	serverInfo := probe.ServerInfo()

	switch platform {
	case types.PlatformAny:
		return nil
	case types.PlatformMCDR:
		if serverInfo.Environments.Mcdr == nil {
			return errors.New("mcdr not found")
		}
		return nil
	default:
		if !serverInfo.Runtime.IsValid() {
			return errors.New("no valid executable found, `lucy add` requires a server in current directory")
		}

		requiredCapability := probe.CapabilityForPlatform(platform)
		if requiredCapability == "" {
			return nil
		}

		topology := serverInfo.Runtime.Topology
		result := probe.EvaluateCompatibility(topology, requiredCapability)
		switch result.Verdict {
		case types.CompatCompatible:
			return nil
		case types.CompatDegraded:
			// CompatDegraded means the ecosystem is reachable only through an indirect
			// hosted/support path. It is warn-only here; numeric risk gating is node-based.
			logger.ShowWarn(
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
