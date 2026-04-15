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
		{"minecraft", RuntimeNodeMinecraft},
		{"Minecraft", RuntimeNodeMinecraft},
		{"  minecraft  ", RuntimeNodeMinecraft},
		{"vanilla", RuntimeNodeMinecraft},
		{"fabric", RuntimeNodeFabric},
		{"Fabric Server", RuntimeNodeFabric},
		{"fabric server", RuntimeNodeFabric},
		{"forge", RuntimeNodeForge},
		{"Forge Server", RuntimeNodeForge},
		{"neoforge", RuntimeNodeNeoforge},
		{"NeoForge Server", RuntimeNodeNeoforge},
		{"mcdr", RuntimeNodeMCDR},
		{"MCDR Plugin", RuntimeNodeMCDR},
		{"paper", RuntimeNodePaper},
		{"spigot", RuntimeNodeSpigot},
		{"velocity", RuntimeNodeVelocity},
		{"bungeecord", RuntimeNodeBungeecord},
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
