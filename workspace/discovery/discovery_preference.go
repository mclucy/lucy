package discovery

import "strings"

type productPreferenceGroup struct {
	names []string
	tier  int
	order int
}

// productPreferences is the fixed live-status preference order: lower tiers,
// then lower orders, win. Names match case-insensitively, and the primary
// runtime name is consulted before the catalogued product string.
var productPreferences = [...]productPreferenceGroup{
	// Tier 0: hybrid server platforms.
	{names: []string{"youer"}, tier: 0, order: 0},
	{names: []string{"arclight", "arclight-forge", "arclight-neoforge", "arclight-fabric"}, tier: 0, order: 1},
	{names: []string{"catserver"}, tier: 0, order: 2},
	{names: []string{"spongeneo"}, tier: 0, order: 3},
	{names: []string{"spongeforge"}, tier: 0, order: 4},

	// Tier 1: mod loaders.
	{names: []string{"neoforge"}, tier: 1, order: 0},
	{names: []string{"forge"}, tier: 1, order: 1},
	{names: []string{"fabric"}, tier: 1, order: 2},

	// Tier 2: Paper-family forks.
	{names: []string{"reaper"}, tier: 2, order: 0},
	{names: []string{"leaf"}, tier: 2, order: 1},
	{names: []string{"divine"}, tier: 2, order: 2},
	{names: []string{"pufferfish"}, tier: 2, order: 3},
	{names: []string{"purpur"}, tier: 2, order: 4},
	{names: []string{"folia"}, tier: 2, order: 5},
	{names: []string{"leaves"}, tier: 2, order: 6},

	// Tier 3: general-purpose server implementations.
	{names: []string{"paper"}, tier: 3, order: 0},
	{names: []string{"spigot"}, tier: 3, order: 1},
	{names: []string{"craftbukkit", "bukkit"}, tier: 3, order: 2},
	{names: []string{"spongevanilla"}, tier: 3, order: 3},

	// Tier 4: vanilla Minecraft.
	{names: []string{"minecraft", "vanilla", "mc"}, tier: 4, order: 0},

	// Tier 5: proxies.
	{names: []string{"bungeecord", "velocity", "waterfall"}, tier: 5, order: 0},
}

const (
	productRankFallbackTier  = 5
	productRankFallbackOrder = 1
)

func productRankByName(name string) (int, int, bool) {
	name = strings.TrimSpace(name)
	for _, group := range productPreferences {
		for _, candidate := range group.names {
			if strings.EqualFold(name, candidate) {
				return group.tier, group.order, true
			}
		}
	}
	return 0, 0, false
}

func productRank(identity Identity) (int, int) {
	if tier, order, ok := productRankByName(identity.Primary.Name.String()); ok {
		return tier, order
	}
	if tier, order, ok := productRankByName(identity.Product().String()); ok {
		return tier, order
	}
	return productRankFallbackTier, productRankFallbackOrder
}
