package bootstrap

import (
	"testing"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

func TestSelectedLoader(t *testing.T) {
	tests := []struct {
		name   string
		offers []workspace.EffectiveEcosystem
		want   types.Ecosystem
	}{
		{
			name:   "no offers",
			offers: nil,
			want:   types.EcoUnspecified,
		},
		{
			name: "vanilla offers nothing loadable",
			offers: []workspace.EffectiveEcosystem{
				{Ecosystem: types.EcoMinecraft, Compatibility: types.CompatFull},
			},
			want: types.EcoUnspecified,
		},
		{
			name: "fabric via primary runtime",
			offers: []workspace.EffectiveEcosystem{
				{Ecosystem: types.EcoFabric, Compatibility: types.CompatFull},
			},
			want: types.EcoFabric,
		},
		{
			name: "arclight claims loader via component offer",
			offers: []workspace.EffectiveEcosystem{
				{Ecosystem: types.EcoFabric, Compatibility: types.CompatFull},
				{Ecosystem: types.EcoBukkit, Compatibility: types.CompatFull},
			},
			want: types.EcoFabric,
		},
		{
			name: "catserver hybrid claims forge without loader component",
			offers: []workspace.EffectiveEcosystem{
				{Ecosystem: types.EcoForge, Compatibility: types.CompatFull},
				{Ecosystem: types.EcoBukkit, Compatibility: types.CompatFull},
			},
			want: types.EcoForge,
		},
		{
			name: "youer hybrid claims neoforge",
			offers: []workspace.EffectiveEcosystem{
				{Ecosystem: types.EcoNeoforge, Compatibility: types.CompatFull},
				{Ecosystem: types.EcoPaper, Compatibility: types.CompatFull},
				{Ecosystem: types.EcoBukkit, Compatibility: types.CompatFull},
			},
			want: types.EcoNeoforge,
		},
		{
			name: "paper server claims no loader",
			offers: []workspace.EffectiveEcosystem{
				{Ecosystem: types.EcoPaper, Compatibility: types.CompatFull},
				{Ecosystem: types.EcoBukkit, Compatibility: types.CompatFull},
			},
			want: types.EcoUnspecified,
		},
		{
			name: "degraded bridge offers do not claim the loader slot",
			offers: []workspace.EffectiveEcosystem{
				{Ecosystem: types.EcoForge, Compatibility: types.CompatFull},
				{Ecosystem: types.EcoFabric, Compatibility: types.CompatDegraded},
			},
			want: types.EcoForge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := selectedLoader(tt.offers); got != tt.want {
				t.Fatalf("selectedLoader() = %q, want %q", got, tt.want)
			}
		})
	}
}
