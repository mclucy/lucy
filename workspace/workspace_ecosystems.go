package workspace

import (
	"slices"
	"strings"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

// ServedEcosystem is one ecosystem that a runtime can serve with a
// compatibility note.
type ServedEcosystem struct {
	Ecosystem     types.Ecosystem     `json:"ecosystem"`
	Compatibility types.Compatibility `json:"compatibility"`
}

// ServedEcosystems lists the package ecosystems a probed workspace
// can serve. The list contains:
//
//   - direct ecosystems from the detected runtime
//   - bridged ecosystems through installed packages. Example: Sinytra
//     Connector
//
// Consumers should call this method. ServerRuntime.servedEcosystems gives
// only the direct ecosystems.
func (ws Workspace) ServedEcosystems() []ServedEcosystem {
	offers := ws.Runtime().servedEcosystems()

	if hasDirectOffer(offers, types.EcosystemForge) ||
		hasDirectOffer(offers, types.EcosystemNeoForge) {
		for _, pkg := range ws.Packages {
			if !isConnectorBridgePackage(pkg) {
				continue
			}
			offers = appendServedEcosystem(
				offers,
				types.EcosystemFabric,
				types.CompatDegraded,
			)
			break
		}
	}

	return offers
}

// servedEcosystems derives offers from the runtime projection only;
// installed packages never contribute.
func (s *ServerRuntime) servedEcosystems() []ServedEcosystem {
	if s == nil || !s.IsValid() {
		return nil
	}

	offers := make([]ServedEcosystem, 0, 3)
	primary := s.Primary
	name := strings.ToLower(strings.TrimSpace(primary.Name.String()))

	switch name {
	case "fabric":
		if primary.Ecosystem == types.EcosystemFabric {
			offers = appendServedEcosystem(
				offers,
				types.EcosystemFabric,
				types.CompatFull,
			)
		}
	case "forge":
		if primary.Ecosystem == types.EcosystemForge {
			offers = appendServedEcosystem(
				offers,
				types.EcosystemForge,
				types.CompatFull,
			)
		}
	case "neoforge":
		if primary.Ecosystem == types.EcosystemNeoForge {
			offers = appendServedEcosystem(
				offers,
				types.EcosystemNeoForge,
				types.CompatFull,
			)
		}
	case "craftbukkit", "bukkit", "spigot":
		offers = appendServedEcosystem(
			offers,
			types.EcosystemBukkit,
			types.CompatFull,
		)
	case "arclight":
		for _, ecosystem := range selectedLoaderEcosystems(
			s.Components,
		) {
			offers = appendServedEcosystem(
				offers,
				ecosystem,
				types.CompatFull,
			)
		}
		if len(offers) > 0 {
			offers = appendServedEcosystem(
				offers,
				types.EcosystemBukkit,
				types.CompatFull,
			)
		}
	case "catserver":
		offers = appendServedEcosystem(
			offers,
			types.EcosystemForge,
			types.CompatFull,
		)
		offers = appendServedEcosystem(
			offers,
			types.EcosystemBukkit,
			types.CompatFull,
		)
	case "youer":
		offers = appendServedEcosystem(
			offers,
			types.EcosystemNeoForge,
			types.CompatFull,
		)
		offers = appendServedEcosystem(
			offers,
			types.EcosystemPaper,
			types.CompatFull,
		)
		offers = appendServedEcosystem(
			offers,
			types.EcosystemBukkit,
			types.CompatFull,
		)
	case "spongevanilla", "spongeforge", "spongeneo":
		offers = appendServedEcosystem(
			offers,
			types.EcosystemSponge,
			types.CompatFull,
		)
		for _, ecosystem := range selectedLoaderEcosystems(
			s.Components,
		) {
			if ecosystem == types.EcosystemFabric {
				continue
			}
			offers = appendServedEcosystem(
				offers,
				ecosystem,
				types.CompatFull,
			)
		}
	case "velocity":
		offers = appendServedEcosystem(
			offers,
			types.EcosystemVelocity,
			types.CompatFull,
		)
	case "bungeecord", "waterfall":
		offers = appendServedEcosystem(
			offers,
			types.EcosystemBungeeCord,
			types.CompatFull,
		)
	default:
		if detector.IsPaperLineage(name) {
			offers = appendServedEcosystem(
				offers,
				types.EcosystemPaper,
				types.CompatFull,
			)
			offers = appendServedEcosystem(
				offers,
				types.EcosystemBukkit,
				types.CompatFull,
			)
		}
	}

	return offers
}

// Supports reports whether pkg runs on the observed runtime. It returns the
// ecosystem that serves pkg and the support level. ok is false when no
// ecosystem serves pkg. A full offer wins over a degraded offer.
func (ws Workspace) Supports(
	pkg types.VersionedPackageRef,
) (offered types.Ecosystem, level types.Compatibility, ok bool) {
	offers := ws.ServedEcosystems()

	serving := -1
	for i, offer := range offers {
		if !offer.Ecosystem.Satisfy(pkg.Ecosystem) {
			continue
		}
		if serving < 0 ||
			offers[serving].Compatibility == types.CompatDegraded &&
				offer.Compatibility == types.CompatFull {
			serving = i
		}
	}
	if serving < 0 {
		return types.EcosystemUnspecified, "", false
	}

	best := offers[serving]
	if best.Compatibility == types.CompatFull {
		return best.Ecosystem, types.CompatFull, true
	}
	return best.Ecosystem, types.CompatDegraded,
		supportsDegraded(best.Ecosystem, pkg)
}

// supportsDegraded validates one package against a degraded offer. The
// default rule accepts every package. Ecosystem-specific checks replace it
// over time. Example: Fabric content over the Sinytra bridge needs a lookup
// in the Probe compatibility report.
func supportsDegraded(eco types.Ecosystem, pkg types.VersionedPackageRef) bool {
	switch eco {
	default:
		return true
	}
}

func appendServedEcosystem(
	offers []ServedEcosystem,
	ecosystem types.Ecosystem,
	compatibility types.Compatibility,
) []ServedEcosystem {
	if ecosystem == types.EcosystemUnspecified {
		return offers
	}
	for i := range offers {
		if offers[i].Ecosystem != ecosystem {
			continue
		}
		if compatibility == types.CompatFull {
			offers[i].Compatibility = compatibility
		}
		return offers
	}
	return append(offers, ServedEcosystem{
		Compatibility: compatibility,
		Ecosystem:     ecosystem,
	})
}

func selectedLoaderEcosystems(
	components []types.VersionedPackageRef,
) []types.Ecosystem {
	var ecosystems []types.Ecosystem
	for _, component := range components {
		name := strings.ToLower(strings.TrimSpace(component.Name.String()))
		switch {
		case component.Ecosystem == types.EcosystemFabric &&
			(name == "fabric-loader" || name == "fabricloader"):
			ecosystems = appendUniqueEcosystem(
				ecosystems,
				types.EcosystemFabric,
			)
		case component.Ecosystem == types.EcosystemForge && name == "forge":
			ecosystems = appendUniqueEcosystem(
				ecosystems,
				types.EcosystemForge,
			)
		case component.Ecosystem == types.EcosystemNeoForge && name == "neoforge":
			ecosystems = appendUniqueEcosystem(
				ecosystems,
				types.EcosystemNeoForge,
			)
		}
	}
	return ecosystems
}

func appendUniqueEcosystem(
	ecosystems []types.Ecosystem,
	ecosystem types.Ecosystem,
) []types.Ecosystem {
	if slices.Contains(ecosystems, ecosystem) {
		return ecosystems
	}
	return append(ecosystems, ecosystem)
}

func hasDirectOffer(
	offers []ServedEcosystem,
	ecosystem types.Ecosystem,
) bool {
	for _, offer := range offers {
		if offer.Ecosystem == ecosystem &&
			offer.Compatibility == types.CompatFull {
			return true
		}
	}
	return false
}

// isConnectorBridgePackage reports whether pkg is Sinytra Connector. The
// mod id varied over time:
//
//   - "connectormod": FML modId of releases up to 1.20.1
//   - "connector": FML modId since 1.21 (renamed per
//     https://github.com/Sinytra/Connector/discussions/1293) and the
//     Modrinth project slug
//   - "sinytra-connector": the CurseForge project slug
func isConnectorBridgePackage(pkg types.DiscoveredPackage) bool {
	if pkg.ID.Ecosystem != types.EcosystemForge &&
		pkg.ID.Ecosystem != types.EcosystemNeoForge {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(pkg.ID.Name.String())) {
	case "sinytra-connector", "connector", "connectormod":
		return true
	default:
		return false
	}
}
