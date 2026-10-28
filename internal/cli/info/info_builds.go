package info

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
)

// infoBuildCriteria describes the runtime a lock declares. An empty
// dimension is unknown and never filters builds.
type infoBuildCriteria struct {
	Minecraft  string
	Ecosystems []catalog.Ecosystem
}

// infoProjectBuilds lists every published build the source can report. The
// project document survives a listing failure; the failure becomes a warning.
func infoProjectBuilds(
	ctx context.Context,
	router *routing.Router,
	ref catalog.Ref,
) ([]infoBuild, []string, error) {
	if !infoProviderUsable(ref.Provider) {
		return nil, nil, nil
	}
	builds, err := router.Builds(ctx, ref)
	if err != nil {
		if failure := infoOperationFailure(ctx, err); failure != nil {
			return nil, nil, failure
		}
		return nil, []string{fmt.Sprintf(
			"could not list builds from %s: %v",
			infoProviderLabel(ref.Provider),
			err,
		)}, nil
	}
	return infoBuildsFromUpstream(builds), nil, nil
}

func infoBuildsFromUpstream(builds []upstream.Build) []infoBuild {
	converted := make([]infoBuild, 0, len(builds))
	for _, build := range builds {
		converted = append(converted, infoBuild{
			Version:      infoCleanLine(build.Version),
			GameVersions: infoCleanList(build.GameVersions),
			Loaders:      infoCleanList(build.Loaders),
			Channel:      infoCleanLine(build.Channel),
			Published:    build.Published,
		})
	}
	return converted
}

func infoCleanList(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if value = infoCleanLine(value); value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}

// infoBuildCriteriaFromLock reads the declared server runtime only. Installed
// packages and observed activity never narrow the available build list.
func infoBuildCriteriaFromLock(lock *document.Lock) *infoBuildCriteria {
	if lock == nil {
		return nil
	}
	criteria := &infoBuildCriteria{
		Minecraft:  infoVersionText(lock.Server.Minecraft),
		Ecosystems: infoLockNativeEcosystems(lock),
	}
	if criteria.Minecraft == "" && len(criteria.Ecosystems) == 0 {
		return nil
	}
	return criteria
}

// infoLockNativeEcosystems reports every ecosystem the locked server product
// serves natively. A product that serves several ecosystems (for example
// Arclight) keeps all of them; filtering never narrows to one.
func infoLockNativeEcosystems(lock *document.Lock) []catalog.Ecosystem {
	product := catalog.VersionedRef{
		Name:    catalog.Name(lock.Server.Product.String()),
		Version: lock.Server.CoreVersion,
	}
	if match, ok := server.NormalizeProduct(product); ok &&
		match.Ecosystem != catalog.EcosystemUnspecified {
		product.Ecosystem = match.Ecosystem
	}

	components := make([]catalog.VersionedRef, 0)
	for _, input := range lock.Server.Inputs {
		for _, module := range input.Modules {
			components = append(components, catalog.VersionedRef{
				Name:      catalog.Name(module.ID),
				Version:   module.Version,
				Ecosystem: module.Ecosystem,
			})
		}
	}

	ecosystems := make([]catalog.Ecosystem, 0, 3)
	seen := make(map[catalog.Ecosystem]struct{}, 3)
	add := func(ecosystem catalog.Ecosystem) {
		if ecosystem == catalog.EcosystemUnspecified {
			return
		}
		if _, ok := seen[ecosystem]; ok {
			return
		}
		seen[ecosystem] = struct{}{}
		ecosystems = append(ecosystems, ecosystem)
	}
	for _, offer := range workspace.ProductServedEcosystems(product, components) {
		if offer.Compatibility != workspace.CompatFull {
			continue
		}
		add(offer.Ecosystem)
	}
	add(product.Ecosystem)
	if lock.MCDR != nil {
		add(catalog.EcosystemMCDR)
	}
	return ecosystems
}

func infoBuildCriteriaText(criteria *infoBuildCriteria) string {
	if criteria == nil {
		return ""
	}
	parts := make([]string, 0, len(criteria.Ecosystems)+1)
	for _, ecosystem := range criteria.Ecosystems {
		if ecosystem == catalog.EcosystemUnspecified {
			continue
		}
		parts = append(parts, ecosystem.Title())
	}
	if criteria.Minecraft != "" {
		parts = append(parts, "Minecraft "+criteria.Minecraft)
	}
	return strings.Join(parts, ", ")
}

// infoBuildCompatible keeps builds that are not known to be incompatible.
// Missing build metadata stays visible: a source that reports no game
// versions or loaders has not proven a mismatch.
func infoBuildCompatible(
	build infoBuild,
	criteria *infoBuildCriteria,
) bool {
	if criteria == nil {
		return true
	}
	if criteria.Minecraft != "" && len(build.GameVersions) > 0 {
		matches := false
		for _, gameVersion := range build.GameVersions {
			if strings.EqualFold(
				strings.TrimSpace(gameVersion),
				criteria.Minecraft,
			) {
				matches = true
				break
			}
		}
		if !matches {
			return false
		}
	}
	return infoBuildMatchesEcosystems(build.Loaders, criteria.Ecosystems)
}

func infoBuildMatchesEcosystems(
	loaders []string,
	required []catalog.Ecosystem,
) bool {
	if len(required) == 0 || len(loaders) == 0 {
		return true
	}
	unknown := false
	for _, loader := range loaders {
		ecosystem, ok := infoBuildEcosystem(loader)
		if !ok {
			unknown = true
			continue
		}
		for _, offer := range required {
			if offer.Satisfy(ecosystem) {
				return true
			}
		}
	}
	return unknown
}

// infoBuildEcosystem maps the loader spellings providers use onto the
// compatibility surfaces Lucy models. Unrecognized loaders stay unknown so
// the human view never claims a proven mismatch.
func infoBuildEcosystem(loader string) (catalog.Ecosystem, bool) {
	switch strings.ToLower(strings.TrimSpace(loader)) {
	case "fabric":
		return catalog.EcosystemFabric, true
	case "forge":
		return catalog.EcosystemForge, true
	case "neoforge":
		return catalog.EcosystemNeoForge, true
	case "paper", "purpur", "folia", "leaves":
		return catalog.EcosystemPaper, true
	case "bukkit", "spigot", "craftbukkit":
		return catalog.EcosystemBukkit, true
	case "sponge", "spongevanilla", "spongeforge", "spongeneo":
		return catalog.EcosystemSponge, true
	case "bungeecord", "waterfall":
		return catalog.EcosystemBungeeCord, true
	case "velocity":
		return catalog.EcosystemVelocity, true
	case "mcdr", "mcdreforged":
		return catalog.EcosystemMCDR, true
	default:
		// "minecraft" usually marks a resource pack or data pack rather than
		// a loader. It stays unknown so a server product is not reported as
		// incompatible with content it can still serve.
		return catalog.EcosystemUnspecified, false
	}
}

// infoBuildsNewestFirst orders the bounded human view. Builds without a
// publication date stay last without inventing an order among themselves.
func infoBuildsNewestFirst(builds []infoBuild) []infoBuild {
	sorted := append([]infoBuild(nil), builds...)
	sort.SliceStable(sorted, func(left, right int) bool {
		return sorted[left].Published.After(sorted[right].Published)
	})
	return sorted
}
