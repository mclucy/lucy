package probe

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestNormalizeRuntimeID_KnownNames(t *testing.T) {
	cases := []struct {
		input string
		want  types.RuntimeNodeID
	}{
		{"minecraft", types.RuntimeNodeMinecraft},
		{"Minecraft", types.RuntimeNodeMinecraft},
		{"  minecraft  ", types.RuntimeNodeMinecraft},
		{"vanilla", types.RuntimeNodeMinecraft},
		{"fabric", types.RuntimeNodeFabric},
		{"Fabric Server", types.RuntimeNodeFabric},
		{"fabric server", types.RuntimeNodeFabric},
		{"forge", types.RuntimeNodeForge},
		{"Forge Server", types.RuntimeNodeForge},
		{"neoforge", types.RuntimeNodeNeoforge},
		{"NeoForge Server", types.RuntimeNodeNeoforge},
		{"mcdr", types.RuntimeNodeMCDR},
		{"MCDR Plugin", types.RuntimeNodeMCDR},
		{"paper", types.RuntimeNodePaper},
		{"spigot", types.RuntimeNodeSpigot},
		{"velocity", types.RuntimeNodeVelocity},
		{"bungeecord", types.RuntimeNodeBungeecord},
		{"geyser", types.RuntimeNodeGeyser},
	}
	for _, tc := range cases {
		got := NormalizeRuntimeID(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeRuntimeID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeRuntimeID_UnknownNames(t *testing.T) {
	cases := []string{
		"unknown_runtime",
		"",
		"   ",
	}
	for _, input := range cases {
		got := NormalizeRuntimeID(input)
		if got != types.RuntimeNodeUnknown {
			t.Errorf("NormalizeRuntimeID(%q) = %q, want RuntimeNodeUnknown", input, got)
		}
	}
}
