package workspace

import (
	"strings"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

// EffectiveEcosystem is one ecosystem that a runtime can serve with a
// compatibility note.
type EffectiveEcosystem struct {
	Ecosystem     types.Ecosystem     `json:"ecosystem"`
	Compatibility types.Compatibility `json:"compatibility"`
}

// EffectiveEcosystems lists the package ecosystems a probed workspace
// can serve. The list contains:
//
//   - direct ecosystems from the detected runtime
//   - bridged ecosystems through installed packages. Example: Sinytra
//     Connector
//
// Consumers should call this method. ServerInstance.effectiveEcosystems
// gives only the direct ecosystems.
func (ws Workspace) EffectiveEcosystems() []EffectiveEcosystem {
	offers := ws.Server().effectiveEcosystems()

	if hasDirectOffer(offers, types.EcoForge) ||
		hasDirectOffer(offers, types.EcoNeoforge) {
		for _, pkg := range ws.Packages {
			if !isConnectorBridgePackage(pkg) {
				continue
			}
			offers = appendEffectiveEcosystem(
				offers,
				types.EcoFabric,
				types.CompatDegraded,
			)
			break
		}
	}

	return offers
}

// effectiveEcosystems is the purely derived from the runtime projection.
// It is not effected by installed packages.
func (s *ServerInstance) effectiveEcosystems() []EffectiveEcosystem {
	if s == nil || !s.IsValid() {
		return nil
	}

	offers := make([]EffectiveEcosystem, 0, 3)
	primary := s.PrimaryRuntime
	name := strings.ToLower(strings.TrimSpace(primary.Name.String()))

	switch name {
	case "fabric":
		if primary.Eco == types.EcoFabric {
			offers = appendEffectiveEcosystem(
				offers,
				types.EcoFabric,
				types.CompatFull,
			)
		}
	case "forge":
		if primary.Eco == types.EcoForge {
			offers = appendEffectiveEcosystem(
				offers,
				types.EcoForge,
				types.CompatFull,
			)
		}
	case "neoforge":
		if primary.Eco == types.EcoNeoforge {
			offers = appendEffectiveEcosystem(
				offers,
				types.EcoNeoforge,
				types.CompatFull,
			)
		}
	case "craftbukkit", "bukkit", "spigot":
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoBukkit,
			types.CompatFull,
		)
	case "arclight":
		for _, ecosystem := range selectedLoaderEcosystems(
			s.RuntimeComponents,
		) {
			offers = appendEffectiveEcosystem(
				offers,
				ecosystem,
				types.CompatFull,
			)
		}
		if len(offers) > 0 {
			offers = appendEffectiveEcosystem(
				offers,
				types.EcoBukkit,
				types.CompatFull,
			)
		}
	case "catserver":
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoForge,
			types.CompatFull,
		)
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoBukkit,
			types.CompatFull,
		)
	case "youer":
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoNeoforge,
			types.CompatFull,
		)
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoPaper,
			types.CompatFull,
		)
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoBukkit,
			types.CompatFull,
		)
	case "spongevanilla", "spongeforge", "spongeneo":
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoSponge,
			types.CompatFull,
		)
		for _, ecosystem := range selectedLoaderEcosystems(
			s.RuntimeComponents,
		) {
			if ecosystem == types.EcoFabric {
				continue
			}
			offers = appendEffectiveEcosystem(
				offers,
				ecosystem,
				types.CompatFull,
			)
		}
	case "velocity":
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoVelocity,
			types.CompatFull,
		)
	case "bungeecord", "waterfall":
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoBungeecord,
			types.CompatFull,
		)
	default:
		if detector.IsPaperFamilyBrand(name) {
			offers = appendEffectiveEcosystem(
				offers,
				types.EcoPaper,
				types.CompatFull,
			)
			offers = appendEffectiveEcosystem(
				offers,
				types.EcoBukkit,
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
	offers := ws.EffectiveEcosystems()

	serving := -1
	for i, offer := range offers {
		if !offer.Ecosystem.Satisfy(pkg.Eco) {
			continue
		}
		if serving < 0 ||
			offers[serving].Compatibility == types.CompatDegraded &&
				offer.Compatibility == types.CompatFull {
			serving = i
		}
	}
	if serving < 0 {
		return types.EcoUnspecified, "", false
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

func appendEffectiveEcosystem(
	offers []EffectiveEcosystem,
	ecosystem types.Ecosystem,
	compatibility types.Compatibility,
) []EffectiveEcosystem {
	if ecosystem == types.EcoUnspecified {
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
	return append(offers, EffectiveEcosystem{
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
		case component.Eco == types.EcoFabric &&
			(name == "fabric-loader" || name == "fabricloader"):
			ecosystems = appendUniqueEcosystem(
				ecosystems,
				types.EcoFabric,
			)
		case component.Eco == types.EcoForge && name == "forge":
			ecosystems = appendUniqueEcosystem(
				ecosystems,
				types.EcoForge,
			)
		case component.Eco == types.EcoNeoforge && name == "neoforge":
			ecosystems = appendUniqueEcosystem(
				ecosystems,
				types.EcoNeoforge,
			)
		}
	}
	return ecosystems
}

func appendUniqueEcosystem(
	ecosystems []types.Ecosystem,
	ecosystem types.Ecosystem,
) []types.Ecosystem {
	for _, existing := range ecosystems {
		if existing == ecosystem {
			return ecosystems
		}
	}
	return append(ecosystems, ecosystem)
}

func hasDirectOffer(
	offers []EffectiveEcosystem,
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
	if pkg.Id.Eco != types.EcoForge &&
		pkg.Id.Eco != types.EcoNeoforge {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(pkg.Id.Name.String())) {
	case "sinytra-connector", "connector", "connectormod":
		return true
	default:
		return false
	}
}
