package install

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
)

func ensureServerPlatformMatch(id types.PackageId) error {
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
		if !serverInfo.Executable.IsValid() {
			return errors.New("no valid executable found, `lucy add` requires a server in current directory")
		}

		requiredCapability := probe.CapabilityForPlatform(platform)
		if requiredCapability == "" {
			return nil
		}

		topology := serverInfo.Executable.Topology
		result := probe.EvaluateCompatibility(topology, requiredCapability)
		switch result.Verdict {
		case types.CompatCompatible:
			return nil
		case types.CompatDegraded:
			if result.RiskLevel < types.RiskHigh {
				logger.ShowWarn(
					fmt.Errorf(
						"compatibility degraded for %s: %s (reason: %s, risk_level: %d)",
						platform,
						result.Detail,
						result.Reason,
						result.RiskLevel,
					),
				)
				return nil
			}

			return fmt.Errorf(
				"%s server not found (reason: %s, verdict: %s, risk_level: %d)",
				platform.Title(),
				result.Reason,
				result.Verdict,
				result.RiskLevel,
			)
		case types.CompatUnresolved:
			return fmt.Errorf(
				"topology unresolved for %s: cannot determine server compatibility",
				platform.Title(),
			)
		case types.CompatIncompatible:
			return fmt.Errorf(
				"%s server not found (reason: %s, verdict: %s, risk_level: %d)",
				platform.Title(),
				result.Reason,
				result.Verdict,
				result.RiskLevel,
			)
		default:
			return fmt.Errorf(
				"%s server not found (reason: %s, verdict: %s, risk_level: %d)",
				platform.Title(),
				result.Reason,
				result.Verdict,
				result.RiskLevel,
			)
		}
	}
}
