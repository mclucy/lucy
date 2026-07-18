package workspace

import "github.com/mclucy/lucy/types"

type RuntimeArtifact struct {
	Identity types.VersionedPackageRef `json:"identity"`
	Path     string                    `json:"path"`
}

type EffectiveEcosystem struct {
	Ecosystem types.Ecosystem     `json:"ecosystem"`
	Verdict   types.CompatVerdict `json:"verdict"`
}

type ServerInstance struct {
	PrimaryRuntime    *RuntimeArtifact            `json:"primary_runtime,omitempty"`
	RuntimeComponents []types.VersionedPackageRef `json:"runtime_components"`
	Packages          []types.DiscoveredPackage   `json:"-"`
}

var UnknownExecutable = &ServerInstance{}

var NoExecutable = &ServerInstance{}

func (s *ServerInstance) IsValid() bool {
	return s != nil &&
		s != NoExecutable &&
		s != UnknownExecutable &&
		s.PrimaryRuntime != nil &&
		s.PrimaryRuntime.Identity.PackageRef != (types.PackageRef{}) &&
		s.PrimaryRuntime.Path != ""
}

func (s *ServerInstance) Analyzable() bool {
	return s.IsValid()
}

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

func (s *ServerInstance) DerivedModLoader() types.Ecosystem {
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

func (s *ServerInstance) DerivedServerCore() string {
	if s == nil || s.PrimaryRuntime == nil {
		return ""
	}
	return s.PrimaryRuntime.Identity.Name.String()
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
