package workspace

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
)

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
			if out[index].Version == normalized.Version {
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
	if !concreteRuntimeVersion(ref.Version) {
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
