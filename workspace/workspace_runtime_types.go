package workspace

import (
	"slices"

	"github.com/mclucy/lucy/types"
)

type ServerInstance struct {
	PrimaryCore     *types.VersionedPackageRef  `json:"primary_core,omitempty"`
	Cores           []types.VersionedPackageRef `json:"cores,omitempty"`
	Packages        []types.DiscoveredPackage   `json:"packages,omitempty"`
	PrimaryEntrance string                      `json:"primary_entrance,omitempty"`
	topology        *types.ServerTopology       `json:"-"`
}

func (s *ServerInstance) PrimaryEcosystem() []types.Ecosystem {
	if s == nil {
		return nil
	}
	if s.PrimaryCore == nil {
		return append([]types.Ecosystem(nil), ecosystemsFromCoreRefs(s.Cores)...)
	}
	return ecosystemsFromCoreRefs([]types.VersionedPackageRef{*s.PrimaryCore})
}

func (s *ServerInstance) RuntimeEcosystems() []types.Ecosystem {
	if s == nil {
		return nil
	}
	return appendUniqueEcosystems(
		nil,
		append(s.PrimaryEcosystem(), s.SecondaryEcosystem()...)...,
	)
}

func (s *ServerInstance) SupportsFabricLoader() bool {
	if s == nil {
		return false
	}
	return slices.Contains(s.RuntimeEcosystems(), types.EcoFabric)
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
	topology: types.TopologyUnknown,
}

var NoExecutable = &ServerInstance{
	topology: types.TopologyEmpty,
}

func (e *ServerInstance) IsValid() bool {
	return e != nil && e != NoExecutable && e != UnknownExecutable && e.PrimaryCore != nil
}

func (e *ServerInstance) Analyzable() bool {
	return e.IsValid()
}

func (s *ServerInstance) DerivedModLoader() types.Ecosystem {
	if s == nil || !s.IsValid() {
		return types.EcoUnspecified
	}
	if core, ok := types.LookupCore(s.PrimaryCore.PackageRef); ok {
		for _, eco := range core.SupportedEcosystems() {
			if modLoaderEcosystem(eco) {
				return eco
			}
		}
	}
	for _, eco := range s.PrimaryEcosystem() {
		if modLoaderEcosystem(eco) {
			return eco
		}
	}
	return types.EcoMinecraft
}

func (s *ServerInstance) DerivedLoaderVersion() string {
	if s == nil || s.PrimaryCore == nil {
		return "unknown"
	}
	loader := s.DerivedModLoader()
	if s.PrimaryCore.Eco == loader || s.PrimaryCore.PackageRef.Eco == loader || primaryCoreSupports(*s.PrimaryCore, loader) {
		if s.PrimaryCore.Version != types.VersionUnknown && s.PrimaryCore.Version != "" {
			return s.PrimaryCore.Version.String()
		}
	}
	return "unknown"
}

func (s *ServerInstance) DerivedServerCore() string {
	if s == nil || s.PrimaryCore == nil {
		return ""
	}
	return s.PrimaryCore.Name.String()
}

func (s *ServerInstance) RefreshPrimaryCore() {
	if s == nil {
		return
	}
	s.PrimaryCore = nil
	for i := range s.Cores {
		if runtimeRefMatchesPrimaryNode(s.Cores[i], s.topology) {
			s.PrimaryCore = &s.Cores[i]
			return
		}
	}
	for i := range s.Cores {
		if _, ok := types.LookupCore(s.Cores[i].PackageRef); ok {
			s.PrimaryCore = &s.Cores[i]
			return
		}
	}
}

func runtimeRefMatchesPrimaryNode(
	ref types.VersionedPackageRef,
	topology *types.ServerTopology,
) bool {
	if topology == nil || !topology.Resolved() {
		return false
	}
	nodeID, ok := RuntimeIdentityNode(ref)
	return ok && nodeID == topology.PrimaryNode
}
