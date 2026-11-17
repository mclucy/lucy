package server

import (
	"testing"

	"github.com/mclucy/lucy/catalog"
)

func TestNormalizeProduct(t *testing.T) {
	tests := []struct {
		name    string
		request catalog.VersionedRef
		want    Product
		ok      bool
	}{
		{
			"bare minecraft alias",
			catalog.VersionedRef{Ref: catalog.Ref{Name: "mc", Provider: catalog.ProviderUnspecified}},
			ProductMinecraft,
			true,
		},
		{
			"case insensitive",
			catalog.VersionedRef{Ref: catalog.Ref{Name: "Paper", Provider: catalog.ProviderUnspecified}},
			ProductPaper,
			true,
		},
		{
			"fabric loader alias",
			catalog.VersionedRef{
				Ref:       catalog.Ref{Name: "fabric-loader", Provider: catalog.ProviderUnspecified},
				Ecosystem: catalog.EcosystemFabric,
			},
			ProductFabric,
			true,
		},
		{
			"sponge forge alias",
			catalog.VersionedRef{
				Ref:       catalog.Ref{Name: "forge", Provider: catalog.ProviderUnspecified},
				Ecosystem: catalog.EcosystemSponge,
			},
			ProductSpongeForge,
			true,
		},
		{
			"regular package",
			catalog.VersionedRef{
				Ref:       catalog.Ref{Name: "fabric-api", Provider: catalog.ProviderUnspecified},
				Ecosystem: catalog.EcosystemFabric,
			},
			"",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeProduct(tt.request)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got.Product != tt.want {
				t.Errorf("product = %q, want %q", got.Product, tt.want)
			}
		})
	}
}
