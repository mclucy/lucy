package detector

import (
	"slices"

	"github.com/mclucy/lucy/types"
)

type bukkitFamilyRank struct {
	priority int
	fallback types.Platform
}

func aggregateBukkitFamilyPackages(pkgs []types.Package) []types.Package {
	if len(pkgs) < 2 {
		return pkgs
	}

	bukkitIndexes := make([]int, 0, len(pkgs))
	for i, pkg := range pkgs {
		if isBukkitFamilyPlatform(pkg.Id.Platform) {
			bukkitIndexes = append(bukkitIndexes, i)
		}
	}

	if len(bukkitIndexes) < 2 {
		return pkgs
	}

	bestIndex := bukkitIndexes[0]
	bestRank := rankBukkitFamilyPlatform(pkgs[bestIndex].Id.Platform)
	for _, idx := range bukkitIndexes[1:] {
		rank := rankBukkitFamilyPlatform(pkgs[idx].Id.Platform)
		if rank.priority > bestRank.priority {
			bestIndex = idx
			bestRank = rank
		}
	}

	aggregated := pkgs[bestIndex]
	aggregated.Supports = mergeBukkitFamilySupport(pkgs, bukkitIndexes, bestRank.fallback)

	resolved := make([]types.Package, 0, len(pkgs)-len(bukkitIndexes)+1)
	inserted := false
	for i, pkg := range pkgs {
		if !isIndexSelected(bukkitIndexes, i) {
			resolved = append(resolved, pkg)
			continue
		}
		if inserted {
			continue
		}
		resolved = append(resolved, aggregated)
		inserted = true
	}

	return resolved
}

func mergeBukkitFamilySupport(
	pkgs []types.Package,
	indexes []int,
	fallback types.Platform,
) *types.PlatformSupport {
	platforms := make([]types.Platform, 0, len(indexes)*2)
	seen := make(map[types.Platform]struct{}, len(indexes)*2+1)
	authentic := false
	versions := make([]types.RawVersion, 0)

	addPlatform := func(platform types.Platform) {
		if !platform.Valid() {
			return
		}
		if _, ok := seen[platform]; ok {
			return
		}
		seen[platform] = struct{}{}
		platforms = append(platforms, platform)
	}

	for _, idx := range indexes {
		pkg := pkgs[idx]
		addPlatform(pkg.Id.Platform)
		if pkg.Supports == nil {
			continue
		}
		authentic = authentic || pkg.Supports.Authentic
		for _, platform := range pkg.Supports.Platforms {
			addPlatform(platform)
		}
		versions = append(versions, pkg.Supports.MinecraftVersions...)
	}

	if len(platforms) == 0 {
		addPlatform(fallback)
	}

	ordered := orderBukkitFamilyPlatforms(platforms)
	if len(ordered) == 0 {
		return nil
	}

	return &types.PlatformSupport{
		MinecraftVersions: versions,
		Platforms:         ordered,
		Authentic:         authentic,
	}
}

func orderBukkitFamilyPlatforms(platforms []types.Platform) []types.Platform {
	ordered := make([]types.Platform, 0, len(platforms))
	for _, candidate := range []types.Platform{
		types.Platform("leaves"),
		types.Platform("folia"),
		types.Platform("paper"),
		types.Platform("spigot"),
		types.Platform("bukkit"),
	} {
		for _, platform := range platforms {
			if platform == candidate {
				ordered = append(ordered, platform)
				break
			}
		}
	}
	return ordered
}

func rankBukkitFamilyPlatform(platform types.Platform) bukkitFamilyRank {
	switch platform {
	case types.Platform("leaves"), types.Platform("folia"):
		return bukkitFamilyRank{priority: 4, fallback: types.Platform("paper")}
	case types.Platform("paper"):
		return bukkitFamilyRank{priority: 3, fallback: types.Platform("paper")}
	case types.Platform("spigot"):
		return bukkitFamilyRank{priority: 2, fallback: types.Platform("spigot")}
	case types.Platform("bukkit"):
		return bukkitFamilyRank{priority: 1, fallback: types.Platform("bukkit")}
	default:
		return bukkitFamilyRank{}
	}
}

func isBukkitFamilyPlatform(platform types.Platform) bool {
	_, ok := bukkitFamilyPlatforms[platform]
	return ok
}

func isIndexSelected(indexes []int, candidate int) bool {
	return slices.Contains(indexes, candidate)
}

var bukkitFamilyPlatforms = map[types.Platform]struct{}{
	types.Platform("bukkit"): {},
	types.Platform("spigot"): {},
	types.Platform("paper"):  {},
	types.Platform("folia"):  {},
	types.Platform("leaves"): {},
}
