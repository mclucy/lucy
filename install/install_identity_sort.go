package install

import (
	"fmt"
	"slices"

	"github.com/mclucy/lucy/types"
)

// sortIdentityPackages sorts identity packages by platform dependency tier.
// Tier 0: Minecraft (base platform)
// Tier 1: Fabric, Forge, NeoForge (mutually exclusive modloaders)
// Tier 2: MCDR (can coexist with anything)
// Within the same tier, input order is preserved.
// Duplicates (same platform) are deduplicated, keeping the first occurrence.
func sortIdentityPackages(ids []types.VersionedPackageRef) []types.VersionedPackageRef {
	// Deduplicate by platform: keep first occurrence of each platform
	seen := make(map[types.PlatformId]bool)
	deduped := make([]types.VersionedPackageRef, 0, len(ids))
	for _, id := range ids {
		platform := id.IdentityToPlatform()
		if !seen[platform] {
			seen[platform] = true
			deduped = append(deduped, id)
		}
	}

	// Sort by tier
	slices.SortStableFunc(
		deduped, func(a, b types.VersionedPackageRef) int {
			tierA := getTier(a.IdentityToPlatform())
			tierB := getTier(b.IdentityToPlatform())
			if tierA < tierB {
				return -1
			}
			if tierA > tierB {
				return 1
			}
			return 0
		},
	)

	return deduped
}

// validateIdentityCompatibility validates that no two incompatible identity packages exist.
// Incompatibility rule: only one tier-1 platform (Fabric, Forge, NeoForge) is allowed.
// Returns nil if valid, or an error describing the conflict.
func validateIdentityCompatibility(ids []types.VersionedPackageRef) error {
	tier1Platforms := make([]types.VersionedPackageRef, 0)

	for _, id := range ids {
		platform := id.IdentityToPlatform()
		tier := getTier(platform)
		if tier == 1 {
			tier1Platforms = append(tier1Platforms, id)
		}
	}

	if len(tier1Platforms) > 1 {
		// Build error message with conflicting platform names
		names := make([]string, len(tier1Platforms))
		for i, id := range tier1Platforms {
			names[i] = string(id.Name)
		}
		return fmt.Errorf(
			"incompatible identity packages: %v (only one modloader allowed)",
			names,
		)
	}

	return nil
}

// getTier returns the dependency tier for a platform.
func getTier(platform types.PlatformId) int {
	switch platform {
	case types.PlatformMinecraft:
		return 0
	case types.PlatformFabric, types.PlatformForge, types.PlatformNeoforge:
		return 1
	case types.PlatformMCDR:
		return 2
	default:
		return 3 // unknown platforms go last
	}
}
