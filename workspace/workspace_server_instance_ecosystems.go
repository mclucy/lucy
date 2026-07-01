package workspace

import (
	"strings"

	"github.com/mclucy/lucy/types"
)

func appendUniqueEcosystems(
	dst []types.Ecosystem,
	add ...types.Ecosystem,
) []types.Ecosystem {
	seen := make(map[types.Ecosystem]struct{}, len(dst))
	for _, eco := range dst {
		seen[eco] = struct{}{}
	}
	for _, eco := range add {
		if eco == types.EcoUnspecified {
			continue
		}
		if _, ok := seen[eco]; ok {
			continue
		}
		seen[eco] = struct{}{}
		dst = append(dst, eco)
	}
	return dst
}

func ecosystemsFromCoreRefs(refs []types.VersionedPackageRef) []types.Ecosystem {
	var out []types.Ecosystem
	for _, ref := range refs {
		if core, ok := types.LookupCore(ref.PackageRef); ok {
			out = appendUniqueEcosystems(out, core.SupportedEcosystems()...)
			continue
		}
		if ref.Eco != types.EcoUnspecified {
			out = appendUniqueEcosystems(out, ref.Eco)
		}
	}
	return out
}

func isConnectorBridgePackage(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sinytra-connector", "connector", "connectormod":
		return true
	default:
		return false
	}
}

func hostCanRunFabricBridge(primary []types.Ecosystem) bool {
	for _, eco := range primary {
		if eco == types.EcoNeoforge || eco == types.EcoForge {
			return true
		}
	}
	return false
}

func secondaryEcosystemsFromPackages(
	primary []types.Ecosystem,
	packages []types.DiscoveredPackage,
) []types.Ecosystem {
	if len(packages) == 0 || !hostCanRunFabricBridge(primary) {
		return nil
	}

	var secondary []types.Ecosystem
	for _, pkg := range packages {
		if !isConnectorBridgePackage(pkg.Id.Name.String()) {
			continue
		}
		secondary = appendUniqueEcosystems(secondary, types.EcoFabric)
	}
	return secondary
}
