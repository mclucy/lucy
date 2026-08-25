package workspace

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

// ServerInstance is the identity of one bootable server runtime. Probe.Single
// produces every instance.
//
// A nil *ServerInstance means that no trustworthy single runtime exists. Every
// non-nil instance satisfies IsValid.
type ServerInstance struct {
	PrimaryRuntime    *types.VersionedPackageRef  `json:"primary_runtime,omitempty"`
	PrimaryPath       string                      `json:"primary_path,omitempty"`
	RuntimeComponents []types.VersionedPackageRef `json:"runtime_components"`
}

// buildServerInstance creates server from detector evidence. It returns nil when
// the evidence cannot name a concrete runtime.
func buildServerInstance(evidence *detector.ExecutableEvidence) *ServerInstance {
	if evidence == nil ||
		evidence.PrimaryRuntime == nil ||
		evidence.PrimaryRuntime.PackageRef == (types.PackageRef{}) ||
		evidence.PrimaryPath == "" {
		return nil
	}

	return &ServerInstance{
		PrimaryRuntime:    evidence.PrimaryRuntime,
		PrimaryPath:       evidence.PrimaryPath,
		RuntimeComponents: normalizeRuntimeComponents(evidence.RuntimeComponents),
	}
}

func (s *ServerInstance) IsValid() bool {
	return s != nil &&
		s.PrimaryRuntime != nil &&
		s.PrimaryRuntime.PackageRef != (types.PackageRef{}) &&
		s.PrimaryPath != ""
}

// GameVersion returns the Minecraft version of the runtime. It returns
// types.VersionUnknown when no component names a concrete version.
func (s *ServerInstance) GameVersion() types.BareVersion {
	if s == nil {
		return types.VersionUnknown
	}
	for _, component := range s.RuntimeComponents {
		if component.Eco != types.EcoMinecraft ||
			component.Name != "minecraft" {
			continue
		}
		if concreteRuntimeVersion(component.Version) {
			return component.Version
		}
	}
	return types.VersionUnknown
}

// ModLoader reports the mod loader ecosystem of the runtime. It
// returns types.EcoMinecraft (vanilla) when no loader applies.
func (s *ServerInstance) ModLoader() types.Ecosystem {
	if s == nil || !s.IsValid() {
		return types.EcoUnspecified
	}
	for _, component := range s.RuntimeComponents {
		if component.Eco == types.EcoFabric &&
			(component.Name == "fabric-loader" ||
				component.Name == "fabricloader") {
			return types.EcoFabric
		}
		if component.Eco == types.EcoForge && component.Name == "forge" {
			return types.EcoForge
		}
		if component.Eco == types.EcoNeoforge &&
			component.Name == "neoforge" {
			return types.EcoNeoforge
		}
	}
	return types.EcoMinecraft
}

// ServerCore returns the name of the primary runtime artifact. An
// example value is "paper".
func (s *ServerInstance) ServerCore() string {
	if s == nil || s.PrimaryRuntime == nil {
		return ""
	}
	return s.PrimaryRuntime.Name.String()
}

func concreteRuntimeVersion(version types.BareVersion) bool {
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
	switch ref.Eco {
	case types.EcoMinecraft:
		if name != "minecraft" {
			return types.VersionedPackageRef{}, false
		}
		ref.Name = "minecraft"
	case types.EcoFabric:
		switch name {
		case "fabric", "fabric-loader", "fabricloader":
			ref.Name = "fabric-loader"
		case "fabric-api":
			ref.Name = "fabric-api"
		default:
			return types.VersionedPackageRef{}, false
		}
	case types.EcoForge:
		if name != "forge" {
			return types.VersionedPackageRef{}, false
		}
		ref.Name = "forge"
	case types.EcoNeoforge:
		if name != "neoforge" {
			return types.VersionedPackageRef{}, false
		}
		ref.Name = "neoforge"
	default:
		return types.VersionedPackageRef{}, false
	}
	return ref, true
}
