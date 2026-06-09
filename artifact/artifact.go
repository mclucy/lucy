package artifact

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path"
	"slices"

	"github.com/mclucy/lucy/types"
)

// Analyze extracts package metadata from an artifact file.
// It opens the file internally and routes to appropriate readers based on file extension.
// For .jar/.zip files, all readers are tried. For .pyz/.mcdr, only the MCDR reader runs.
func Analyze(filePath string, opts ...Option) ([]ArtifactInfo, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	ext := path.Ext(filePath)
	switch ext {
	case ".jar", ".zip", ".pyz", ".mcdr":
	default:
		return nil, fmt.Errorf("unsupported artifact format: %s", ext)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat artifact: %w", err)
	}

	zipReader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}

	var results []ArtifactInfo

	if ext == ".pyz" || ext == ".mcdr" {
		mcdrReader := newMcdrReader()
		results, err := mcdrReader.Read(zipReader, filePath, o.slugResolver)
		if err != nil {
			return nil, err
		}
		applySlugResolver(results, o.slugResolver)
		return results, nil
	}

	for _, r := range readers {
		infos, err := r.Read(zipReader, filePath, o.slugResolver)
		if err != nil {
			continue
		}
		results = append(results, infos...)
	}
	applySlugResolver(results, o.slugResolver)

	if jarPlatformsConflict(results) {
		return nil, fmt.Errorf(
			"ambiguous artifact %q: packages span incompatible ecosystems",
			filePath,
		)
	}

	results = aggregateBukkitFamilyPackages(results)
	return results, nil
}

func applySlugResolver(results []ArtifactInfo, resolver SlugResolver) {
	if resolver == nil {
		return
	}

	ctx := context.Background()
	for i := range results {
		normalized, err := resolver(
			ctx,
			results[i].Ref.Platform,
			results[i].Ref.Name,
		)
		if err == nil && normalized != "" {
			results[i].Ref.Name = types.PackageName(normalized)
		}
	}
}

// jarPlatformsConflict returns true when the detected artifacts span two or more
// ecosystem families that cannot coexist in a single deployable JAR.
//
// Ecosystem families:
//
//	proxyFamily  – velocity, bungeecord
//	serverFamily – bukkit, paper, leaves, folia, spigot
//	modFamily    – fabric, forge, neoforge
//
// PlatformAny artifacts are excluded from the conflict check.
func jarPlatformsConflict(infos []ArtifactInfo) bool {
	if len(infos) == 0 {
		return false
	}

	proxyPlatforms := map[types.Platform]struct{}{
		types.Platform("velocity"):   {},
		types.Platform("bungeecord"): {},
	}
	serverPlatforms := map[types.Platform]struct{}{
		types.Platform("bukkit"): {},
		types.Platform("paper"):  {},
		types.Platform("leaves"): {},
		types.Platform("folia"):  {},
		types.Platform("spigot"): {},
	}
	modPlatforms := map[types.Platform]struct{}{
		types.PlatformFabric:   {},
		types.PlatformForge:    {},
		types.PlatformNeoforge: {},
	}

	var hasProxy, hasServer, hasMod bool
	for _, info := range infos {
		p := info.Ref.Platform
		if p == types.PlatformAny {
			continue
		}
		if _, ok := proxyPlatforms[p]; ok {
			hasProxy = true
		}
		if _, ok := serverPlatforms[p]; ok {
			hasServer = true
		}
		if _, ok := modPlatforms[p]; ok {
			hasMod = true
		}
	}

	families := 0
	if hasProxy {
		families++
	}
	if hasServer {
		families++
	}
	if hasMod {
		families++
	}
	return families > 1
}

type bukkitFamilyRank struct {
	priority int
	fallback types.Platform
}

func aggregateBukkitFamilyPackages(infos []ArtifactInfo) []ArtifactInfo {
	if len(infos) < 2 {
		return infos
	}

	bukkitIndexes := make([]int, 0, len(infos))
	for i, info := range infos {
		if isBukkitFamilyPlatform(info.Ref.Platform) {
			bukkitIndexes = append(bukkitIndexes, i)
		}
	}

	if len(bukkitIndexes) < 2 {
		return infos
	}

	bestIndex := bukkitIndexes[0]
	bestRank := rankBukkitFamilyPlatform(infos[bestIndex].Ref.Platform)
	for _, idx := range bukkitIndexes[1:] {
		rank := rankBukkitFamilyPlatform(infos[idx].Ref.Platform)
		if rank.priority > bestRank.priority {
			bestIndex = idx
			bestRank = rank
		}
	}

	aggregated := infos[bestIndex]
	aggregated.Supports = mergeBukkitFamilySupport(
		infos,
		bukkitIndexes,
		bestRank.fallback,
	)

	resolved := make([]ArtifactInfo, 0, len(infos)-len(bukkitIndexes)+1)
	inserted := false
	for i, info := range infos {
		if !isIndexSelected(bukkitIndexes, i) {
			resolved = append(resolved, info)
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
	infos []ArtifactInfo,
	indexes []int,
	fallback types.Platform,
) *types.PlatformSupport {
	platforms := make([]types.Platform, 0, len(indexes)*2)
	seen := make(map[types.Platform]struct{}, len(indexes)*2+1)
	authentic := false
	versions := make([]types.BareVersion, 0)

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
		info := infos[idx]
		addPlatform(info.Ref.Platform)
		if info.Supports == nil {
			continue
		}
		authentic = authentic || info.Supports.Authentic
		for _, platform := range info.Supports.Platforms {
			addPlatform(platform)
		}
		versions = append(versions, info.Supports.MinecraftVersions...)
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
