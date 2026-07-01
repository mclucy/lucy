package artifact

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/types"
)

// Analyze extracts package metadata from an artifact file.
// It opens the file internally and routes to appropriate readers based on file extension.
// For .jar/.zip files, all readers are tried. For .pyz/.mcdr, only the MCDR reader runs.
func Analyze(filePath string, opts ...Option) ([]Info, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	ext := filepath.Ext(filePath)
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

	var results []Info

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

	if jarEcosystemsConflict(results) {
		return nil, fmt.Errorf(
			"ambiguous artifact %q: packages span incompatible ecosystems",
			filePath,
		)
	}

	results = aggregateBukkitFamilyPackages(results)
	return results, nil
}

func applySlugResolver(results []Info, resolver SlugResolver) {
	if resolver == nil {
		return
	}

	ctx := context.Background()
	for i := range results {
		normalized, err := resolver(
			ctx,
			results[i].Ref.Eco,
			results[i].Ref.Name,
		)
		if err == nil && normalized != "" {
			results[i].Ref.Name = types.BarePackageName(normalized)
		}
	}
}

// jarEcosystemsConflict returns true when the detected artifacts span two or more
// ecosystem families that cannot coexist in a single deployable JAR.
//
// Ecosystem families:
//
//	proxyFamily  – velocity, bungeecord
//	serverFamily – bukkit, paper, leaves, folia, spigot
//	modFamily    – fabric, forge, neoforge
//
// PlatformAny artifacts are excluded from the conflict check.
func jarEcosystemsConflict(infos []Info) bool {
	if len(infos) == 0 {
		return false
	}

	proxyPlatforms := map[types.Ecosystem]struct{}{
		types.Ecosystem("velocity"):   {},
		types.Ecosystem("bungeecord"): {},
	}
	serverPlatforms := map[types.Ecosystem]struct{}{
		types.Ecosystem("bukkit"): {},
		types.Ecosystem("paper"):  {},
		types.Ecosystem("leaves"): {},
		types.Ecosystem("folia"):  {},
		types.Ecosystem("spigot"): {},
	}
	modPlatforms := map[types.Ecosystem]struct{}{
		types.EcoFabric:   {},
		types.EcoForge:    {},
		types.EcoNeoforge: {},
	}

	var hasProxy, hasServer, hasMod bool
	for _, info := range infos {
		p := info.Ref.Eco
		if p == types.EcoUnspecified {
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
	fallback types.Ecosystem
}

func aggregateBukkitFamilyPackages(infos []Info) []Info {
	if len(infos) < 2 {
		return infos
	}

	bukkitIndexes := make([]int, 0, len(infos))
	for i, info := range infos {
		if isBukkitFamilyEcosystem(info.Ref.Eco) {
			bukkitIndexes = append(bukkitIndexes, i)
		}
	}

	if len(bukkitIndexes) < 2 {
		return infos
	}

	bestIndex := bukkitIndexes[0]
	bestRank := rankBukkitFamilyEcosystem(infos[bestIndex].Ref.Eco)
	for _, idx := range bukkitIndexes[1:] {
		rank := rankBukkitFamilyEcosystem(infos[idx].Ref.Eco)
		if rank.priority > bestRank.priority {
			bestIndex = idx
			bestRank = rank
		}
	}

	aggregated := infos[bestIndex]

	resolved := make([]Info, 0, len(infos)-len(bukkitIndexes)+1)
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

func rankBukkitFamilyEcosystem(platform types.Ecosystem) bukkitFamilyRank {
	switch platform {
	case types.Ecosystem("leaves"), types.Ecosystem("folia"):
		return bukkitFamilyRank{
			priority: 4, fallback: types.Ecosystem("paper"),
		}
	case types.Ecosystem("paper"):
		return bukkitFamilyRank{
			priority: 3, fallback: types.Ecosystem("paper"),
		}
	case types.Ecosystem("spigot"):
		return bukkitFamilyRank{
			priority: 2, fallback: types.Ecosystem("spigot"),
		}
	case types.Ecosystem("bukkit"):
		return bukkitFamilyRank{
			priority: 1, fallback: types.Ecosystem("bukkit"),
		}
	default:
		return bukkitFamilyRank{}
	}
}

func isBukkitFamilyEcosystem(platform types.Ecosystem) bool {
	_, ok := bukkitFamilyEcosystems[platform]
	return ok
}

func isIndexSelected(indexes []int, candidate int) bool {
	return slices.Contains(indexes, candidate)
}

var bukkitFamilyEcosystems = map[types.Ecosystem]struct{}{
	types.Ecosystem("bukkit"): {},
	types.Ecosystem("spigot"): {},
	types.Ecosystem("paper"):  {},
	types.Ecosystem("folia"):  {},
	types.Ecosystem("leaves"): {},
}
