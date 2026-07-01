package workspace

import (
	"github.com/mclucy/lucy/types"
)

type ServerInstance struct {
	PrimaryEntrance string                      `json:"primary_entrance"`
	Cores           []types.VersionedPackageRef `json:"cores,omitempty"`
	Packages        []types.DiscoveredPackage   `json:"packages,omitempty"`
	topology        *types.ServerTopology       `json:"-"`
}

func (s *ServerInstance) PrimaryEcosystem() []types.Ecosystem {
	if s == nil {
		return nil
	}
	return append([]types.Ecosystem(nil), ecosystemsFromCoreRefs(s.Cores)...)
}

func (s *ServerInstance) SecondaryEcosystem() []types.Ecosystem {
	if s == nil {
		return nil
	}
	primary := s.PrimaryEcosystem()
	return append(
		[]types.Ecosystem(nil),
		secondaryEcosystemsFromPackages(primary, s.Packages)...,
	)
}

func (s *ServerInstance) GameVersion() types.BareVersion {
	if s == nil {
		return types.VersionUnknown
	}
	for _, ref := range s.Cores {
		if ref.Eco != types.EcoMinecraft &&
			ref.Name != "minecraft" &&
			ref.Name != "mc" {
			continue
		}
		if ref.Version != types.VersionUnknown &&
			ref.Version != types.VersionNone &&
			ref.Version != "" {
			return ref.Version
		}
	}
	return types.VersionUnknown
}

var UnknownExecutable = &ServerInstance{
	PrimaryEntrance: "",
	topology:        types.TopologyUnknown,
}

var NoExecutable = &ServerInstance{
	PrimaryEntrance: "",
	topology:        types.TopologyEmpty,
}

func (e *ServerInstance) IsValid() bool {
	return e != nil && e.topology != nil
}

func (e *ServerInstance) Analyzable() bool {
	return e != nil && e.topology != nil && len(e.Cores) > 0 &&
		e != NoExecutable && e != UnknownExecutable
}
