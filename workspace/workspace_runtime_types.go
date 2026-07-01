package workspace

import (
	"github.com/mclucy/lucy/types"
)

type ServerInstance struct {
	PrimaryEntrance string                      `json:"primary_entrance"`
	Cores           []types.VersionedPackageRef `json:"cores,omitempty"`
	Packages        []types.DiscoveredPackage   `json:"packages,omitempty"`
	topology        *types.ServerTopology       `json:"-"`

	primaryEcosystems   []types.Ecosystem
	secondaryEcosystems []types.Ecosystem
}

func (s *ServerInstance) PrimaryEcosystem() []types.Ecosystem {
	if s == nil {
		return nil
	}
	if len(s.primaryEcosystems) > 0 {
		return append([]types.Ecosystem(nil), s.primaryEcosystems...)
	}
	if len(s.Cores) == 0 {
		return nil
	}
	seen := map[types.Ecosystem]struct{}{}
	var out []types.Ecosystem
	add := func(ecos []types.Ecosystem) {
		for _, eco := range ecos {
			if eco == types.EcoUnspecified {
				continue
			}
			if _, ok := seen[eco]; ok {
				continue
			}
			seen[eco] = struct{}{}
			out = append(out, eco)
		}
	}
	for _, ref := range s.Cores {
		if core, ok := types.LookupCore(ref.PackageRef); ok {
			add(core.SupportedEcosystems())
			continue
		}
		if ref.Eco != types.EcoUnspecified {
			add([]types.Ecosystem{ref.Eco})
		}
	}
	return out
}

func (s *ServerInstance) SecondaryEcosystem() []types.Ecosystem {
	if s == nil {
		return nil
	}
	return append([]types.Ecosystem(nil), s.secondaryEcosystems...)
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
