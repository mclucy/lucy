package workspace

import (
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/workspace/discovery"
)

// ServedEcosystem is one ecosystem that a runtime can serve with a
// compatibility note.
type ServedEcosystem struct {
	Ecosystem     catalog.Ecosystem `json:"ecosystem"`
	Compatibility Compatibility     `json:"compatibility"`
}

// ServedEcosystems lists the package ecosystems an observed workspace
// can serve. The list contains:
//
//   - direct ecosystems from the detected runtime
//   - bridged ecosystems through installed packages. Example: Sinytra
//     Connector
//
// Consumers should call this method. selectionServedEcosystems gives only the
// direct ecosystems.
func (ws *Workspace) ServedEcosystems() []ServedEcosystem {
	if ws == nil {
		return nil
	}
	offers := selectionServedEcosystems(ws.Runtime())

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

// ProductServedEcosystems derives native offers from a server product and its
// loader components, independent of installed packages.
func ProductServedEcosystems(
	product catalog.VersionedRef,
	components []catalog.VersionedRef,
) []ServedEcosystem {
	return productServedEcosystems(product.Name.String(), product, components)
}

// selectionServedEcosystems derives offers from the selected installation
// identity only; installed packages never contribute.
func selectionServedEcosystems(
	selection *discovery.Selection,
) []ServedEcosystem {
	if selection == nil || !selection.IsValid() || selection.Installation == nil {
		return nil
	}

	identity := selection.Installation.Identity
	return productServedEcosystems(
		identity.Primary.Name.String(),
		identity.Primary,
		identity.Components,
	)
}

func productServedEcosystems(
	name string,
	primary catalog.VersionedRef,
	components []catalog.VersionedRef,
) []ServedEcosystem {
	offers := make([]ServedEcosystem, 0, 3)
	name = strings.ToLower(strings.TrimSpace(name))

	// Hybrid products bridge ecosystems that no loader table row expresses,
	// such as Arclight serving both Forge and Bukkit.
	switch name {
	case "arclight", "arclight-forge", "arclight-neoforge", "arclight-fabric":
		switch name {
		case "arclight-forge":
			offers = appendServedEcosystem(offers, catalog.EcosystemForge, CompatFull)
		case "arclight-neoforge":
			offers = appendServedEcosystem(offers, catalog.EcosystemNeoForge, CompatFull)
		case "arclight-fabric":
			offers = appendServedEcosystem(offers, catalog.EcosystemFabric, CompatFull)
		default:
			for _, ecosystem := range selectedLoaderEcosystems(components) {
				offers = appendServedEcosystem(offers, ecosystem, CompatFull)
			}
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
			components,
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
		offers = appendProductOffers(offers, primary, name)
	}

	return offers
}

// appendProductOffers adds the ecosystems a product serves on its own. A
// product without a package family serves no packages, unless its name
// resolves to one that does.
func appendProductOffers(
	offers []ServedEcosystem,
	primary catalog.VersionedRef,
	name string,
) []ServedEcosystem {
	ecosystem := primary.Ecosystem
	if ecosystem.IsUnspecified() {
		if product, ok := serverpkg.ParseProduct(name); ok {
			ecosystem = product.Ecosystem()
		}
	}
	if ecosystem.Family() == catalog.FamilyNone {
		return offers
	}
	for _, served := range ecosystem.Loads() {
		offers = appendServedEcosystem(offers, served, CompatFull)
	}
	return offers
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
		if _, ok := serverpkg.LoaderComponentName(
			component.Ecosystem,
			component.Name.String(),
		); !ok {
			continue
		}
		ecosystems = appendUniqueEcosystem(ecosystems, component.Ecosystem)
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
