package server

import (
	"testing"

	"github.com/mclucy/lucy/catalog"
)

func TestProductDefinitionsInvariants(t *testing.T) {
	seenProducts := make(map[Product]bool)
	seenAliases := make(map[productAlias]Product)
	for _, definition := range productDefinitions {
		if seenProducts[definition.Product] {
			t.Errorf("duplicate definition for product %q", definition.Product)
		}
		seenProducts[definition.Product] = true
		for _, alias := range definition.Aliases {
			if alias.Name == "" {
				t.Errorf("product %q has empty alias", definition.Product)
			}
			if alias.Name != catalog.Name(lowercaseName(alias.Name)) {
				t.Errorf("product %q alias %q is not lowercase", definition.Product, alias.Name)
			}
			if owner, exists := seenAliases[alias]; exists {
				t.Errorf("alias %q/%q claimed by %q and %q", alias.Ecosystem, alias.Name, owner, definition.Product)
			}
			seenAliases[alias] = definition.Product
		}
	}
}

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

func lowercaseName(name catalog.Name) string {
	lowered := ""
	for _, r := range string(name) {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		lowered += string(r)
	}
	return lowered
}
