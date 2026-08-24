package workspace

import "github.com/mclucy/lucy/types"

type EffectiveEcosystem struct {
	Ecosystem     types.Ecosystem     `json:"ecosystem"`
	Compatibility types.Compatibility `json:"compatibility"`
}

type ServerInstance struct {
	PrimaryRuntime    *types.VersionedPackageRef  `json:"primary_runtime,omitempty"`
	PrimaryPath       string                      `json:"primary_path,omitempty"`
	RuntimeComponents []types.VersionedPackageRef `json:"runtime_components"`
	Packages          []types.DiscoveredPackage   `json:"-"`
}

var UnknownServer = &ServerInstance{}

var NoServer = &ServerInstance{}

func (s *ServerInstance) IsValid() bool {
	return s != nil &&
		s != NoServer &&
		s != UnknownServer &&
		s.PrimaryRuntime != nil &&
		s.PrimaryRuntime.PackageRef != (types.PackageRef{}) &&
		s.PrimaryPath != ""
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
