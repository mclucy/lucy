package workspace

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

// ServerRuntime is the identity of one bootable server runtime. Probe.Single
// produces every runtime.
//
// A nil *ServerRuntime means that no trustworthy single runtime exists. Every
// non-nil runtime satisfies IsValid.
type ServerRuntime struct {
	Primary     *types.VersionedPackageRef  `json:"primary_runtime,omitempty"`
	PrimaryPath string                      `json:"primary_path,omitempty"`
	Components  []types.VersionedPackageRef `json:"runtime_components"`
}

// buildServerRuntime creates runtime from detector evidence. It returns nil
// when the evidence cannot name a concrete runtime.
func buildServerRuntime(evidence *detector.RuntimeEvidence) *ServerRuntime {
	if evidence == nil ||
		evidence.PrimaryRuntime == nil ||
		evidence.PrimaryRuntime.PackageRef == (types.PackageRef{}) ||
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
		s.Primary.PackageRef != (types.PackageRef{}) &&
		s.PrimaryPath != ""
}

// MinecraftVersion returns the Minecraft version of the runtime. It returns
// types.VersionUnknown when no component names a concrete version.
func (s *ServerRuntime) MinecraftVersion() types.Version {
	if s == nil {
		return types.VersionUnknown
	}
	for _, component := range s.Components {
		if component.Ecosystem != types.EcosystemMinecraft ||
			component.Name != "minecraft" {
			continue
		}
		if concreteRuntimeVersion(component.Version) {
			return component.Version
		}
	}
	return types.VersionUnknown
}

// Ecosystem reports the mod loader ecosystem of the runtime. It
// returns types.EcosystemMinecraft when no loader applies.
func (s *ServerRuntime) Ecosystem() types.Ecosystem {
	if s == nil || !s.IsValid() {
		return types.EcosystemUnspecified
	}
	for _, component := range s.Components {
		if component.Ecosystem == types.EcosystemFabric &&
			(component.Name == "fabric-loader" ||
				component.Name == "fabricloader") {
			return types.EcosystemFabric
		}
		if component.Ecosystem == types.EcosystemForge && component.Name == "forge" {
			return types.EcosystemForge
		}
		if component.Ecosystem == types.EcosystemNeoForge &&
			component.Name == "neoforge" {
			return types.EcosystemNeoForge
		}
	}
	return types.EcosystemMinecraft
}

// Product returns the executable server product of the primary runtime. It
// returns types.ServerProductUnknown when the primary runtime name is not a
// known server product.
func (s *ServerRuntime) Product() types.ServerProduct {
	if s == nil || s.Primary == nil {
		return types.ServerProductUnknown
	}
	product, ok := types.ParseServerProduct(s.Primary.Name.String())
	if !ok {
		return types.ServerProductUnknown
	}
	return product
}

func concreteRuntimeVersion(version types.Version) bool {
	switch version {
	case "", types.VersionNone, types.VersionUnknown, types.VersionAny,
		types.VersionStable, types.VersionBeta:
		return false
	default:
		return true
	}
}

// normalizeRuntimeComponents converts raw detector components into a stable
// list. The rules are:
//   - names are canonical
//   - duplicate base identities merge; a concrete version replaces
//     types.VersionUnknown
//   - conflicting concrete versions are both dropped, with an error report
func normalizeRuntimeComponents(
	components []types.VersionedPackageRef,
) []types.VersionedPackageRef {
	indexes := make(map[string]int, len(components))
	conflicts := make(map[string]struct{})
	out := make([]types.VersionedPackageRef, 0, len(components))
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
				normalized.Version == types.VersionUnknown {
				continue
			}
			if out[index].Version == types.VersionUnknown {
				out[index] = normalized
				continue
			}
			log.Error(fmt.Errorf(
				"conflicting selected runtime component versions for %s: %s and %s",
				key,
				out[index].Version,
				normalized.Version,
			))
			out[index] = types.VersionedPackageRef{}
			delete(indexes, key)
			conflicts[key] = struct{}{}
			continue
		}
		indexes[key] = len(out)
		out = append(out, normalized)
	}

	write := 0
	for _, component := range out {
		if component.PackageRef == (types.PackageRef{}) {
			continue
		}
		out[write] = component
		write++
	}
	return out[:write]
}

func normalizeRuntimeComponent(
	ref types.VersionedPackageRef,
) (types.VersionedPackageRef, bool) {
	if !concreteRuntimeVersion(ref.Version) &&
		ref.Version != types.VersionUnknown {
		return types.VersionedPackageRef{}, false
	}

	name := strings.ToLower(ref.Name.String())
	switch ref.Ecosystem {
	case types.EcosystemMinecraft:
		if name != "minecraft" {
			return types.VersionedPackageRef{}, false
		}
		ref.Name = "minecraft"
	case types.EcosystemFabric:
		switch name {
		case "fabric", "fabric-loader", "fabricloader":
			ref.Name = "fabric-loader"
		case "fabric-api":
			ref.Name = "fabric-api"
		default:
			return types.VersionedPackageRef{}, false
		}
	case types.EcosystemForge:
		if name != "forge" {
			return types.VersionedPackageRef{}, false
		}
		ref.Name = "forge"
	case types.EcosystemNeoForge:
		if name != "neoforge" {
			return types.VersionedPackageRef{}, false
		}
		ref.Name = "neoforge"
	default:
		return types.VersionedPackageRef{}, false
	}
	return ref, true
}
