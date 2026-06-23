package routing

import (
	"slices"
	"testing"

	"github.com/mclucy/lucy/types"
)

func topologyWithCapability(capability types.RuntimeCapability) *types.RuntimeTopology {
	return &types.RuntimeTopology{
		PrimaryNode: types.RuntimeNodeBukkit,
		Nodes: []types.RuntimeNode{
			{
				ID:           types.RuntimeNodeBukkit,
				Role:         types.RuntimeRolePluginCore,
				Capabilities: []types.RuntimeCapability{capability},
			},
		},
	}
}

func topologyWithCapabilities(capabilities ...types.RuntimeCapability) *types.RuntimeTopology {
	return &types.RuntimeTopology{
		PrimaryNode: types.RuntimeNodeBukkit,
		Nodes: []types.RuntimeNode{
			{
				ID:           types.RuntimeNodeBukkit,
				Role:         types.RuntimeRolePluginCore,
				Capabilities: capabilities,
			},
		},
	}
}

func TestIsBukkitFamilyCapability(t *testing.T) {
	cases := []struct {
		name       string
		capability types.RuntimeCapability
		want       bool
	}{
		{"bukkit", types.CapabilityBukkitPlugins, true},
		{"spigot", types.CapabilitySpigotPlugins, true},
		{"paper", types.CapabilityPaperPlugins, true},
		{"purpur", types.CapabilityPurpurPlugins, true},
		{"folia", types.CapabilityFoliaPlugins, true},
		{"fabric mods", types.CapabilityFabricMods, false},
		{"forge mods", types.CapabilityForgeMods, false},
		{"neoforge mods", types.CapabilityNeoforgeMods, false},
		{"mcdr", types.CapabilityMCDRPlugins, false},
		{"velocity", types.CapabilityVelocityPlugins, false},
		{"bungeecord", types.CapabilityBungeecordPlugins, false},
		{"sponge", types.CapabilitySpongePlugins, false},
		{"proxying", types.CapabilityProxying, false},
		{"protocol bridge", types.CapabilityProtocolBridge, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := isBukkitFamilyCapability(tt.capability)
			if got != tt.want {
				t.Fatalf(
					"isBukkitFamilyCapability(%q) = %v, want %v",
					tt.capability,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestProviderSourcesFromTopology_BukkitFamily(t *testing.T) {
	curseforgePresent := DefaultRegistry().has(types.SourceCurseForge)

	cases := []struct {
		name       string
		capability types.RuntimeCapability
	}{
		{"bukkit plugins", types.CapabilityBukkitPlugins},
		{"spigot plugins", types.CapabilitySpigotPlugins},
		{"paper plugins", types.CapabilityPaperPlugins},
		{"purpur plugins", types.CapabilityPurpurPlugins},
		{"folia plugins", types.CapabilityFoliaPlugins},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resolution := providerSourcesFromTopology(topologyWithCapability(tt.capability))

			if resolution.fallback {
				t.Fatalf(
					"unexpected fallback for Bukkit-family capability %q",
					tt.capability,
				)
			}
			if resolution.empty {
				t.Fatalf(
					"unexpected empty resolution for Bukkit-family capability %q",
					tt.capability,
				)
			}

			mustContain := []types.SourceId{
				types.SourceModrinth,
				types.SourceHangar,
				types.SourceSpiget,
			}
			for _, source := range mustContain {
				if !slices.Contains(resolution.sources, source) {
					t.Errorf(
						"expected source %s in result, got %v",
						source,
						resolution.sources,
					)
				}
			}

			if slices.Contains(resolution.sources, types.SourceCurseForge) {
				t.Errorf(
					"CurseForge must never appear for Bukkit-family routing, got %v",
					resolution.sources,
				)
			}

			if slices.Contains(resolution.sources, types.SourceMCDR) {
				t.Errorf(
					"MCDR must never appear for Bukkit-family routing, got %v",
					resolution.sources,
				)
			}

			if curseforgePresent {
				t.Logf(
					"curseforge available in this build; verified it stays excluded for %q",
					tt.capability,
				)
			}

			expectedOrder := []types.SourceId{
				types.SourceModrinth,
				types.SourceHangar,
				types.SourceSpiget,
			}
			if !slices.Equal(resolution.sources, expectedOrder) {
				t.Errorf(
					"source order changed for %q: got %v, want %v",
					tt.capability,
					resolution.sources,
					expectedOrder,
				)
			}
		})
	}
}

func TestProviderSourcesFromTopology_ModLoaders(t *testing.T) {
	curseforgePresent := DefaultRegistry().has(types.SourceCurseForge)

	cases := []struct {
		name       string
		capability types.RuntimeCapability
	}{
		{"fabric mods", types.CapabilityFabricMods},
		{"forge mods", types.CapabilityForgeMods},
		{"neoforge mods", types.CapabilityNeoforgeMods},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resolution := providerSourcesFromTopology(topologyWithCapability(tt.capability))

			if resolution.fallback {
				t.Fatalf("unexpected fallback for mod-loader capability %q", tt.capability)
			}
			if resolution.empty {
				t.Fatalf("unexpected empty resolution for mod-loader capability %q", tt.capability)
			}
			if !slices.Contains(resolution.sources, types.SourceModrinth) {
				t.Errorf(
					"expected Modrinth for mod-loader capability %q, got %v",
					tt.capability,
					resolution.sources,
				)
			}

			hasCurse := slices.Contains(resolution.sources, types.SourceCurseForge)
			if curseforgePresent && !hasCurse {
				t.Errorf(
					"expected CurseForge for mod-loader %q (available in build), got %v",
					tt.capability,
					resolution.sources,
				)
			}
			if !curseforgePresent && hasCurse {
				t.Errorf(
					"unexpected CurseForge for mod-loader %q (not available in build), got %v",
					tt.capability,
					resolution.sources,
				)
			}

			if slices.Contains(resolution.sources, types.SourceHangar) {
				t.Errorf(
					"Hangar must not appear for mod-loader capability %q, got %v",
					tt.capability,
					resolution.sources,
				)
			}
			if slices.Contains(resolution.sources, types.SourceSpiget) {
				t.Errorf(
					"Spiget must not appear for mod-loader capability %q, got %v",
					tt.capability,
					resolution.sources,
				)
			}
		})
	}
}

func TestProviderSourcesFromTopology_MCDR(t *testing.T) {
	resolution := providerSourcesFromTopology(
		topologyWithCapability(types.CapabilityMCDRPlugins),
	)

	if resolution.fallback {
		t.Fatal("unexpected fallback for MCDR capability")
	}
	if resolution.empty {
		t.Fatal("unexpected empty resolution for MCDR capability")
	}

	want := []types.SourceId{types.SourceMCDR}
	if !slices.Equal(resolution.sources, want) {
		t.Errorf("MCDR routing: got %v, want %v", resolution.sources, want)
	}
}

func TestProviderSourcesFromTopology_ProxyOnly(t *testing.T) {
	resolution := providerSourcesFromTopology(
		topologyWithCapability(types.CapabilityProxying),
	)

	if !resolution.empty {
		t.Errorf(
			"proxy-only topology should produce empty sources, got %v",
			resolution.sources,
		)
	}
	if resolution.fallback {
		t.Error("proxy-only topology must not trigger fallback")
	}
}

func TestProviderSourcesFromTopology_NonBukkitPluginCapabilities(t *testing.T) {
	// Velocity, Bungeecord, Sponge, ProtocolBridge are not part of the
	// Bukkit-family routing bucket. A node with only one of these must
	// produce no sources and must trigger fallback (no sawKnownCapability).
	cases := []struct {
		name       string
		capability types.RuntimeCapability
	}{
		{"velocity", types.CapabilityVelocityPlugins},
		{"bungeecord", types.CapabilityBungeecordPlugins},
		{"sponge", types.CapabilitySpongePlugins},
		{"protocol bridge", types.CapabilityProtocolBridge},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resolution := providerSourcesFromTopology(topologyWithCapability(tt.capability))

			if len(resolution.sources) != 0 {
				t.Errorf(
					"%s capability must not produce sources, got %v",
					tt.name,
					resolution.sources,
				)
			}
			if !resolution.fallback {
				t.Errorf(
					"%s capability must trigger fallback (unknown capability)",
					tt.name,
				)
			}
		})
	}
}

func TestProviderSourcesFromTopology_MultipleBukkitFamilyNodes(t *testing.T) {
	// Multiple Bukkit-family nodes (e.g. Paper primary + Folia bridge) must
	// not produce duplicate sources and must keep the canonical order.
	topology := &types.RuntimeTopology{
		PrimaryNode: types.RuntimeNodePaper,
		Nodes: []types.RuntimeNode{
			{
				ID:   types.RuntimeNodePaper,
				Role: types.RuntimeRolePluginCore,
				Capabilities: []types.RuntimeCapability{
					types.CapabilityPaperPlugins,
				},
			},
			{
				ID:   types.RuntimeNodeFolia,
				Role: types.RuntimeRolePluginCore,
				Capabilities: []types.RuntimeCapability{
					types.CapabilityFoliaPlugins,
				},
			},
		},
	}

	resolution := providerSourcesFromTopology(topology)

	if resolution.fallback || resolution.empty {
		t.Fatalf(
			"expected concrete sources for multi-node Bukkit-family topology, got fallback=%v empty=%v sources=%v",
			resolution.fallback,
			resolution.empty,
			resolution.sources,
		)
	}

	want := []types.SourceId{
		types.SourceModrinth,
		types.SourceHangar,
		types.SourceSpiget,
	}
	if !slices.Equal(resolution.sources, want) {
		t.Errorf("multi-node Bukkit-family: got %v, want %v", resolution.sources, want)
	}
}

func TestProviderSourcesFromTopology_MixedBukkitAndModLoader(t *testing.T) {
	// A single node with both FabricMods and PaperPlugins must include
	// Modrinth (both), Hangar + Spiget (Paper), and CurseForge (Fabric) when
	// available. CurseForge should never appear attached to the Bukkit-family
	// portion.
	resolution := providerSourcesFromTopology(
		topologyWithCapabilities(
			types.CapabilityFabricMods,
			types.CapabilityPaperPlugins,
		),
	)

	curseforgePresent := DefaultRegistry().has(types.SourceCurseForge)

	mustContain := []types.SourceId{
		types.SourceModrinth,
		types.SourceHangar,
		types.SourceSpiget,
	}
	for _, source := range mustContain {
		if !slices.Contains(resolution.sources, source) {
			t.Errorf("expected source %s, got %v", source, resolution.sources)
		}
	}

	hasCurse := slices.Contains(resolution.sources, types.SourceCurseForge)
	if curseforgePresent && !hasCurse {
		t.Errorf(
			"expected CurseForge for mixed topology (Fabric + Paper) when available, got %v",
			resolution.sources,
		)
	}
}
