package workspace

import (
	"strings"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

func (s *ServerInstance) EffectiveEcosystems() []EffectiveEcosystem {
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
				types.CompatCompatible,
			)
		}
	case "forge":
		if primary.Eco == types.EcoForge {
			offers = appendEffectiveEcosystem(
				offers,
				types.EcoForge,
				types.CompatCompatible,
			)
		}
	case "neoforge":
		if primary.Eco == types.EcoNeoforge {
			offers = appendEffectiveEcosystem(
				offers,
				types.EcoNeoforge,
				types.CompatCompatible,
			)
		}
	case "craftbukkit", "bukkit", "spigot":
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoBukkit,
			types.CompatCompatible,
		)
	case "arclight":
		for _, ecosystem := range selectedLoaderEcosystems(
			s.RuntimeComponents,
		) {
			offers = appendEffectiveEcosystem(
				offers,
				ecosystem,
				types.CompatCompatible,
			)
		}
		if len(offers) > 0 {
			offers = appendEffectiveEcosystem(
				offers,
				types.EcoBukkit,
				types.CompatCompatible,
			)
		}
	case "catserver":
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoForge,
			types.CompatCompatible,
		)
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoBukkit,
			types.CompatCompatible,
		)
	case "youer":
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoNeoforge,
			types.CompatCompatible,
		)
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoPaper,
			types.CompatCompatible,
		)
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoBukkit,
			types.CompatCompatible,
		)
	case "spongevanilla", "spongeforge", "spongeneo":
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoSponge,
			types.CompatCompatible,
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
				types.CompatCompatible,
			)
		}
	case "velocity":
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoVelocity,
			types.CompatCompatible,
		)
	case "bungeecord", "waterfall":
		offers = appendEffectiveEcosystem(
			offers,
			types.EcoBungeecord,
			types.CompatCompatible,
		)
	default:
		if detector.IsPaperFamilyBrand(name) {
			offers = appendEffectiveEcosystem(
				offers,
				types.EcoPaper,
				types.CompatCompatible,
			)
			offers = appendEffectiveEcosystem(
				offers,
				types.EcoBukkit,
				types.CompatCompatible,
			)
		}
	}

	if hasDirectOffer(offers, types.EcoForge) ||
		hasDirectOffer(offers, types.EcoNeoforge) {
		for _, pkg := range s.Packages {
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
		if compatibility == types.CompatCompatible {
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
			offer.Compatibility == types.CompatCompatible {
			return true
		}
	}
	return false
}

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
