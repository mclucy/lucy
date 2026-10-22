package workspace

import (
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

// ServedEcosystem is one ecosystem that a runtime can serve with a
// compatibility note.
type ServedEcosystem struct {
	Ecosystem     catalog.Ecosystem `json:"ecosystem"`
	Compatibility Compatibility     `json:"compatibility"`
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

	if hasDirectOffer(offers, catalog.EcosystemForge) ||
		hasDirectOffer(offers, catalog.EcosystemNeoForge) {
		for _, pkg := range ws.Packages {
			if !isConnectorBridgePackage(pkg) {
				continue
			}
			offers = appendServedEcosystem(
				offers,
				catalog.EcosystemFabric,
				CompatDegraded,
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
		if primary.Ecosystem == catalog.EcosystemFabric {
			offers = appendServedEcosystem(
				offers,
				catalog.EcosystemFabric,
				CompatFull,
			)
		}
	case "forge":
		if primary.Ecosystem == catalog.EcosystemForge {
			offers = appendServedEcosystem(
				offers,
				catalog.EcosystemForge,
				CompatFull,
			)
		}
	case "neoforge":
		if primary.Ecosystem == catalog.EcosystemNeoForge {
			offers = appendServedEcosystem(
				offers,
				catalog.EcosystemNeoForge,
				CompatFull,
			)
		}
	case "craftbukkit", "bukkit", "spigot":
		offers = appendServedEcosystem(
			offers,
			catalog.EcosystemBukkit,
			CompatFull,
		)
	case "arclight":
		for _, ecosystem := range selectedLoaderEcosystems(
			s.Components,
		) {
			offers = appendServedEcosystem(
				offers,
				ecosystem,
				CompatFull,
			)
		}
		if len(offers) > 0 {
			offers = appendServedEcosystem(
				offers,
				catalog.EcosystemBukkit,
				CompatFull,
			)
		}
	case "catserver":
		offers = appendServedEcosystem(
			offers,
			catalog.EcosystemForge,
			CompatFull,
		)
		offers = appendServedEcosystem(
			offers,
			catalog.EcosystemBukkit,
			CompatFull,
		)
	case "youer":
		offers = appendServedEcosystem(
			offers,
			catalog.EcosystemNeoForge,
			CompatFull,
		)
		offers = appendServedEcosystem(
			offers,
			catalog.EcosystemPaper,
			CompatFull,
		)
		offers = appendServedEcosystem(
			offers,
			catalog.EcosystemBukkit,
			CompatFull,
		)
	case "spongevanilla", "spongeforge", "spongeneo":
		offers = appendServedEcosystem(
			offers,
			catalog.EcosystemSponge,
			CompatFull,
		)
		for _, ecosystem := range selectedLoaderEcosystems(
			s.Components,
		) {
			if ecosystem == catalog.EcosystemFabric {
				continue
			}
			offers = appendServedEcosystem(
				offers,
				ecosystem,
				CompatFull,
			)
		}
	case "velocity":
		offers = appendServedEcosystem(
			offers,
			catalog.EcosystemVelocity,
			CompatFull,
		)
	case "bungeecord", "waterfall":
		offers = appendServedEcosystem(
			offers,
			catalog.EcosystemBungeeCord,
			CompatFull,
		)
	default:
		if detector.IsPaperLineage(name) {
			offers = appendServedEcosystem(
				offers,
				catalog.EcosystemPaper,
				CompatFull,
			)
			offers = appendServedEcosystem(
				offers,
				catalog.EcosystemBukkit,
				CompatFull,
			)
		}
	}

	return offers
}

// Supports reports whether pkg runs on the observed runtime. It returns the
// ecosystem that serves pkg and the support level. ok is false when no
// ecosystem serves pkg. A full offer wins over a degraded offer.
func (ws Workspace) Supports(
	pkg catalog.VersionedRef,
) (offered catalog.Ecosystem, level Compatibility, ok bool) {
	offers := ws.ServedEcosystems()

	serving := -1
	for i, offer := range offers {
		if !offer.Ecosystem.Satisfy(pkg.Ecosystem) {
			continue
		}
		if serving < 0 ||
			offers[serving].Compatibility == CompatDegraded &&
				offer.Compatibility == CompatFull {
			serving = i
		}
	}
	if serving < 0 {
		return catalog.EcosystemUnspecified, "", false
	}

	best := offers[serving]
	if best.Compatibility == CompatFull {
		return best.Ecosystem, CompatFull, true
	}
	return best.Ecosystem, CompatDegraded,
		supportsDegraded(best.Ecosystem, pkg)
}

// supportsDegraded validates one package against a degraded offer. The
// default rule accepts every package. Ecosystem-specific checks replace it
// over time. Example: Fabric content over the Sinytra bridge needs a lookup
// in the Probe compatibility report.
func supportsDegraded(eco catalog.Ecosystem, pkg catalog.VersionedRef) bool {
	switch eco {
	default:
		return true
	}
}

func appendServedEcosystem(
	offers []ServedEcosystem,
	ecosystem catalog.Ecosystem,
	compatibility Compatibility,
) []ServedEcosystem {
	if ecosystem == catalog.EcosystemUnspecified {
		return offers
	}
	for i := range offers {
		if offers[i].Ecosystem != ecosystem {
			continue
		}
		if compatibility == CompatFull {
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
	components []catalog.VersionedRef,
) []catalog.Ecosystem {
	var ecosystems []catalog.Ecosystem
	for _, component := range components {
		name := strings.ToLower(strings.TrimSpace(component.Name.String()))
		switch {
		case component.Ecosystem == catalog.EcosystemFabric &&
			(name == "fabric-loader" || name == "fabricloader"):
			ecosystems = appendUniqueEcosystem(
				ecosystems,
				catalog.EcosystemFabric,
			)
		case component.Ecosystem == catalog.EcosystemForge && name == "forge":
			ecosystems = appendUniqueEcosystem(
				ecosystems,
				catalog.EcosystemForge,
			)
		case component.Ecosystem == catalog.EcosystemNeoForge && name == "neoforge":
			ecosystems = appendUniqueEcosystem(
				ecosystems,
				catalog.EcosystemNeoForge,
			)
		}
	}
	return ecosystems
}

func appendUniqueEcosystem(
	ecosystems []catalog.Ecosystem,
	ecosystem catalog.Ecosystem,
) []catalog.Ecosystem {
	if slices.Contains(ecosystems, ecosystem) {
		return ecosystems
	}
	return append(ecosystems, ecosystem)
}

func hasDirectOffer(
	offers []ServedEcosystem,
	ecosystem catalog.Ecosystem,
) bool {
	for _, offer := range offers {
		if offer.Ecosystem == ecosystem &&
			offer.Compatibility == CompatFull {
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
func isConnectorBridgePackage(pkg DiscoveredPackage) bool {
	if pkg.ID.Ecosystem != catalog.EcosystemForge &&
		pkg.ID.Ecosystem != catalog.EcosystemNeoForge {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(pkg.ID.Name.String())) {
	case "sinytra-connector", "connector", "connectormod":
		return true
	default:
		return false
	}
}
