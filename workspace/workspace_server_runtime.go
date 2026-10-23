package workspace

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace/discovery"
)

// ServerRuntime is the identity of one bootable server runtime. Probe.Single
// produces every runtime.
//
// A nil *ServerRuntime means that no trustworthy single runtime exists. Every
// non-nil runtime satisfies IsValid.
type ServerRuntime struct {
	Primary     *catalog.VersionedRef  `json:"primary_runtime,omitempty"`
	PrimaryPath string                 `json:"primary_path,omitempty"`
	Components  []catalog.VersionedRef `json:"runtime_components"`
}

// buildServerRuntime creates runtime from detector evidence. It returns nil
// when the evidence cannot name a concrete runtime.
func buildServerRuntime(evidence *discovery.RuntimeEvidence) *ServerRuntime {
	if evidence == nil ||
		evidence.PrimaryRuntime == nil ||
		evidence.PrimaryRuntime.Ref == (catalog.Ref{}) ||
		evidence.PrimaryPath == "" {
		return nil
	}

	return &ServerRuntime{
		Primary:     evidence.PrimaryRuntime,
		PrimaryPath: evidence.PrimaryPath,
		Components:  normalizeRuntimeComponents(evidence.RuntimeComponents),
	}
}

func (s *ServerRuntime) IsValid() bool {
	return s != nil &&
		s.Primary != nil &&
		s.Primary.Ref != (catalog.Ref{}) &&
		s.PrimaryPath != ""
}

// MinecraftVersion returns the Minecraft version of the runtime. It returns
// version.Unknown when no component names a concrete version.
func (s *ServerRuntime) MinecraftVersion() versionpkg.Version {
	if s == nil {
		return versionpkg.Unknown
	}
	for _, component := range s.Components {
		if component.Ecosystem != catalog.EcosystemMinecraft ||
			component.Name != "minecraft" {
			continue
		}
		if concreteRuntimeVersion(component.Version) {
			return component.Version
		}
	}
	return versionpkg.Unknown
}

// Ecosystem reports the mod loader ecosystem of the runtime. It
// returns catalog.EcosystemMinecraft when no loader applies.
func (s *ServerRuntime) Ecosystem() catalog.Ecosystem {
	if s == nil || !s.IsValid() {
		return catalog.EcosystemUnspecified
	}
	for _, component := range s.Components {
		if component.Ecosystem == catalog.EcosystemFabric &&
			(component.Name == "fabric-loader" ||
				component.Name == "fabricloader") {
			return catalog.EcosystemFabric
		}
		if component.Ecosystem == catalog.EcosystemForge && component.Name == "forge" {
			return catalog.EcosystemForge
		}
		if component.Ecosystem == catalog.EcosystemNeoForge &&
			component.Name == "neoforge" {
			return catalog.EcosystemNeoForge
		}
	}
	return catalog.EcosystemMinecraft
}

// Product returns the executable server product of the primary runtime. It
// returns server.ProductUnknown when the primary runtime name is not a
// known server product.
func (s *ServerRuntime) Product() server.Product {
	if s == nil || s.Primary == nil {
		return server.ProductUnknown
	}
	product, ok := server.ParseProduct(s.Primary.Name.String())
	if !ok {
		return server.ProductUnknown
	}
	return product
}

func concreteRuntimeVersion(version versionpkg.Version) bool {
	switch version {
	case "", versionpkg.None, versionpkg.Unknown, versionpkg.Any,
		versionpkg.Stable, versionpkg.Beta:
		return false
	default:
		return true
	}
}

// normalizeRuntimeComponents converts raw detector components into a stable
// list. The rules are:
//   - names are canonical
//   - duplicate base identities merge; a concrete version replaces
//     version.Unknown
//   - conflicting concrete versions are both dropped, with an error report
func normalizeRuntimeComponents(
	components []catalog.VersionedRef,
) []catalog.VersionedRef {
	indexes := make(map[string]int, len(components))
	conflicts := make(map[string]struct{})
	out := make([]catalog.VersionedRef, 0, len(components))
	for _, component := range components {
		normalized, ok := normalizeRuntimeComponent(component)
		if !ok {
			continue
		}
		key := normalized.StringBase()
		if _, conflicted := conflicts[key]; conflicted {
			continue
		}
		if index, exists := indexes[key]; exists {
			if out[index].Version == normalized.Version ||
				normalized.Version == versionpkg.Unknown {
				continue
			}
			if out[index].Version == versionpkg.Unknown {
				out[index] = normalized
				continue
			}
			log.Error(fmt.Errorf(
				"conflicting selected runtime component versions for %s: %s and %s",
				key,
				out[index].Version,
				normalized.Version,
			))
			out[index] = catalog.VersionedRef{}
			delete(indexes, key)
			conflicts[key] = struct{}{}
			continue
		}
		indexes[key] = len(out)
		out = append(out, normalized)
	}

	write := 0
	for _, component := range out {
		if component.Ref == (catalog.Ref{}) {
			continue
		}
		out[write] = component
		write++
	}
	return out[:write]
}

func normalizeRuntimeComponent(
	ref catalog.VersionedRef,
) (catalog.VersionedRef, bool) {
	if !concreteRuntimeVersion(ref.Version) &&
		ref.Version != versionpkg.Unknown {
		return catalog.VersionedRef{}, false
	}

	name := strings.ToLower(ref.Name.String())
	switch ref.Ecosystem {
	case catalog.EcosystemMinecraft:
		if name != "minecraft" {
			return catalog.VersionedRef{}, false
		}
		ref.Name = "minecraft"
	case catalog.EcosystemFabric:
		switch name {
		case "fabric", "fabric-loader", "fabricloader":
			ref.Name = "fabric-loader"
		case "fabric-api":
			ref.Name = "fabric-api"
		default:
			return catalog.VersionedRef{}, false
		}
	case catalog.EcosystemForge:
		if name != "forge" {
			return catalog.VersionedRef{}, false
		}
		ref.Name = "forge"
	case catalog.EcosystemNeoForge:
		if name != "neoforge" {
			return catalog.VersionedRef{}, false
		}
		ref.Name = "neoforge"
	default:
		return catalog.VersionedRef{}, false
	}
	return ref, true
}
